// Package receive routes each client message through its middleware to the use cases, and chooses the reply.
package receive

import (
	"context"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"

	"finga/internal/application/gateway/send"
	"finga/internal/application/gateway/session"
	"finga/internal/handler/gateway/ws/push"
	"finga/pkg/panics"
	"finga/pkg/wsconn"
	"finga/pkg/wsrouter"
)

const tracerName = "finga/internal/handler/gateway/ws/receive"

const (
	commandTimeout = 5 * time.Second
	failureLogGap  = 10 * time.Second // a command that answers nothing logs its unexpected failures at most this often
)

// Options tune the receiving side; Logger, Panics and Traces are required.
type Options struct {
	HelloTimeout time.Duration // a connection that sends no Hello in this long is closed
	CommandQueue int           // commands waiting their turn, and as many actions; one more answers rate_limited
	Logger       *slog.Logger
	// Panics records a panic in a message's chain.
	Panics *panics.Recorder
	// Traces starts each queued command's trace.
	Traces trace.TracerProvider
}

// Handler routes every connection's messages to gw's use cases.
type Handler struct {
	gw       *send.Gateway
	opts     Options
	router   *wsrouter.Router[*Inbox]
	failures *rate.Sometimes
	tracer   trace.Tracer
}

// New routes messages to gw.
func New(gw *send.Gateway, opts Options) *Handler {
	h := &Handler{gw: gw, opts: opts, failures: &rate.Sometimes{First: 1, Interval: failureLogGap}, tracer: opts.Traces.Tracer(tracerName)}
	h.router = h.routes()
	return h
}

// Inbox is one connection's receiving side: Hello first, the rate limits, and two queues, each run in order on its own goroutine. Commands run one at a time; actions (moves, attacks, skills, portals and teleports) run apart from them, so a command waiting on another module never holds the player still. An action never waits on a module, and keeps its order with the other actions: the world counts a press from the input after it.
type Inbox struct {
	out        *push.Outbox
	session    *session.Session
	helloTimer *time.Timer

	// Owned by the read loop.
	helloSeen bool
	limiters  map[string]*rate.Limiter

	// One goroutine runs queued commands in order, another the actions once Hello is answered; Stop cancels ctx and waits for both.
	queue   chan func()
	actions chan func()
	ctx     context.Context
	cancel  context.CancelFunc
	drained chan struct{} // the command goroutine ended
	acted   chan struct{} // the action goroutine ended
	hello   chan struct{} // closed once Hello is answered, joined or not
	joined  bool          // set by Hello before hello closes; read after it
}

// Open starts a connection's receiving side for s: its Hello deadline and its command goroutine.
func (h *Handler) Open(out *push.Outbox, s *session.Session) *Inbox {
	in := &Inbox{out: out, session: s, limiters: map[string]*rate.Limiter{}, queue: make(chan func(), h.opts.CommandQueue), actions: make(chan func(), h.opts.CommandQueue),
		drained: make(chan struct{}), acted: make(chan struct{}), hello: make(chan struct{})}
	in.ctx, in.cancel = context.WithCancel(context.Background())
	in.helloTimer = time.AfterFunc(h.opts.HelloTimeout, func() {
		h.opts.Panics.Do(in.ctx, panics.FlowWS, func() { out.Close(wsconn.CloseKicked, "no hello") }, "conn", out.ID())
	})
	h.opts.Panics.Go(in.ctx, panics.FlowWS, in.commands, "conn", out.ID())
	h.opts.Panics.Go(in.ctx, panics.FlowWS, in.runActions, "conn", out.ID())
	return in
}

// Dispatch routes a message on the read loop.
func (h *Handler) Dispatch(in *Inbox, m wsconn.Message) {
	h.router.Dispatch(in, wsrouter.Message{Type: m.Type, ReqID: m.ReqID, Seq: m.Seq, Data: m.Data})
}

// Stop ends the Hello deadline, cancels the connection's commands and actions and waits until none runs.
func (in *Inbox) Stop() {
	in.helloTimer.Stop()
	in.cancel()
	<-in.drained
	<-in.acted
}

// commands runs the queued commands in order until Stop.
func (in *Inbox) commands() {
	defer close(in.drained)
	in.run(in.queue)
}

// runActions runs the queued actions in order once Hello is answered, as they once queued behind it, until Stop.
func (in *Inbox) runActions() {
	defer close(in.acted)
	select {
	case <-in.ctx.Done():
		return
	case <-in.hello:
	}
	in.run(in.actions)
}

// run runs q's chains in order until Stop; a panic outside a chain closes the socket, so the player reconnects.
func (in *Inbox) run(q <-chan func()) {
	stopped := false
	defer func() {
		if !stopped {
			in.out.Close(wsconn.CloseInternal, "internal error")
		}
	}()
	for {
		select {
		case <-in.ctx.Done():
			stopped = true
			return
		case rest := <-q:
			if in.ctx.Err() != nil {
				stopped = true
				return
			}
			rest()
		}
	}
}

// allow takes a token from the connection's bucket for l's group, sized for the world's tick rate as the session knows it.
func (in *Inbox) allow(l limit, now time.Time) bool {
	r, burst := l.at(in.session.TickRateHz())
	b, ok := in.limiters[l.group]
	switch {
	case !ok:
		b = rate.NewLimiter(r, burst)
		in.limiters[l.group] = b
	case b.Limit() != r:
		b.SetLimitAt(now, r)
		b.SetBurstAt(now, burst)
	}
	return b.AllowN(now, 1)
}
