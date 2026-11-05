// Package receive routes each client message through its middleware to the gateway's use cases,
// and chooses the reply; the push package writes it.
package receive

import (
	"context"
	"log/slog"
	"time"

	"golang.org/x/time/rate"

	"finga/pkg/panics"
	"finga/pkg/wsconn"
	"finga/pkg/wsrouter"
	"finga/services/gateway/internal/application/send"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/handler/ws/push"
)

const (
	commandTimeout = 5 * time.Second
	failureLogGap  = 10 * time.Second // a command that answers nothing logs its unexpected failures at most this often
)

// Options tune the receiving side; Logger and Panics are required.
type Options struct {
	HelloTimeout time.Duration // a connection that sends no Hello in this long is closed
	CommandQueue int           // commands waiting their turn; one more answers rate_limited
	Logger       *slog.Logger
	// Panics records a panic in a message's chain.
	Panics *panics.Recorder
}

// Handler routes every connection's messages to gw's use cases.
type Handler struct {
	gw       *send.Gateway
	opts     Options
	router   *wsrouter.Router[*Inbox]
	failures *rate.Sometimes
}

// New routes messages to gw.
func New(gw *send.Gateway, opts Options) *Handler {
	h := &Handler{gw: gw, opts: opts, failures: &rate.Sometimes{First: 1, Interval: failureLogGap}}
	h.router = h.routes()
	return h
}

// Inbox is one connection's receiving side: Hello first, the rate limits and the command queue.
type Inbox struct {
	out        *push.Outbox
	session    *session.Session
	helloTimer *time.Timer

	// Owned by the read loop.
	helloSeen bool
	limiters  map[string]*rate.Limiter

	// One goroutine runs queued chains in order; Stop cancels ctx and waits for drained.
	queue   chan func()
	ctx     context.Context
	cancel  context.CancelFunc
	drained chan struct{}
	joined  bool // owned by the command goroutine
}

// Open starts a connection's receiving side for s: its Hello deadline and its command goroutine.
func (h *Handler) Open(out *push.Outbox, s *session.Session) *Inbox {
	in := &Inbox{out: out, session: s, limiters: map[string]*rate.Limiter{}, queue: make(chan func(), h.opts.CommandQueue), drained: make(chan struct{})}
	in.ctx, in.cancel = context.WithCancel(context.Background())
	in.helloTimer = time.AfterFunc(h.opts.HelloTimeout, func() { out.Close(wsconn.CloseKicked, "no hello") })
	go in.commands()
	return in
}

// Dispatch routes a message on the read loop.
func (h *Handler) Dispatch(in *Inbox, m wsconn.Message) {
	h.router.Dispatch(in, wsrouter.Message{Type: m.Type, ReqID: m.ReqID, Seq: m.Seq, Data: m.Data})
}

// Stop ends the Hello deadline, cancels the connection's commands and waits until none runs.
func (in *Inbox) Stop() {
	in.helloTimer.Stop()
	in.cancel()
	<-in.drained
}

func (in *Inbox) commands() {
	defer close(in.drained)
	for {
		select {
		case <-in.ctx.Done():
			return
		case rest := <-in.queue:
			if in.ctx.Err() != nil {
				return
			}
			rest()
		}
	}
}

// allow takes a token from the connection's bucket for l's group.
func (in *Inbox) allow(l limit, now time.Time) bool {
	b, ok := in.limiters[l.group]
	if !ok {
		b = rate.NewLimiter(rate.Limit(l.perSecond), l.burst)
		in.limiters[l.group] = b
	}
	return b.AllowN(now, 1)
}
