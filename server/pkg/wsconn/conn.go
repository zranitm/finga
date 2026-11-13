package wsconn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"finga/pkg/panics"
	"finga/pkg/uuidv7"
)

// Close codes; the 4000s are the layer's own.
const (
	CloseInternal      = int(websocket.StatusInternalError)
	CloseNoSubprotocol = int(websocket.StatusPolicyViolation) // the client asked for none of the server's subprotocols
	CloseDropped       = int(websocket.StatusAbnormalClosure) // never sent: what a browser reports for a connection that ended with no close frame
	CloseSlowConsumer  = 4001
	CloseTimeout       = 4002
	CloseKicked        = 4003 // the handler ended the session
	CloseBadMessage    = 4004
)

// Defaults for Options.
const (
	DefaultReadLimit    = 16 << 10
	DefaultSendQueue    = 256
	DefaultPingInterval = 20 * time.Second
	DefaultPongTimeout  = 10 * time.Second
	writeTimeout        = 10 * time.Second
	goodbyeTimeout      = 2 * time.Second // the last frames, the close frame and the peer's answer
)

// The connection's errors.
var (
	ErrQueueFull = errors.New("wsconn: send queue full")
	ErrClosed    = errors.New("wsconn: connection closed")
	// ErrEncode is a message the codec could not encode, a programming error; it closes with 1011.
	ErrEncode = errors.New("wsconn: message cannot be encoded")
)

// CloseError is why we closed a connection.
type CloseError struct {
	Code   int
	Reason string // sent in the close frame
	Err    error  // our side's cause, for the logs; never sent
}

func (e *CloseError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("wsconn: closed %d: %s: %v", e.Code, e.Reason, e.Err)
	}
	return fmt.Sprintf("wsconn: closed %d: %s", e.Code, e.Reason)
}

// Unwrap is the cause.
func (e *CloseError) Unwrap() error { return e.Err }

// PeerClose is the peer's close frame, a normal closure included, so the handler can log its code.
type PeerClose struct {
	Code   int
	Reason string
}

func (e *PeerClose) Error() string {
	return fmt.Sprintf("wsconn: peer closed %d: %s", e.Code, e.Reason)
}

// Conn is a live connection as the handler sees it.
type Conn interface {
	ID() string
	// Send enqueues m without blocking, so m must not change after; a full queue or unencodable m ends the connection.
	Send(m Message) error
	// Close ends the connection; the first call wins.
	Close(code int, reason string)
}

// Handler callbacks run serially on the read goroutine; OnClose runs last, once, with a zero state if OnOpen panicked.
// OnClose's error is how the connection ended: a *CloseError when we closed it, a *PeerClose when the peer did, else why it dropped with no close frame.
type Handler[S any] interface {
	OnOpen(Conn) S
	OnMessage(Conn, S, Message)
	OnClose(Conn, S, error)
}

// Subprotocol is a codec a client asks for by name when it connects.
type Subprotocol struct {
	Name  string
	Codec Codec
}

// Options tune Serve. Zero values take the defaults.
type Options struct {
	// Subprotocols are the codecs a client picks from, in the server's order of preference; a client that asks for none is closed.
	Subprotocols   []Subprotocol
	AllowedOrigins []string // full origins, like http://localhost:5173; empty: same host only
	ReadLimit      int64
	SendQueue      int
	PingInterval   time.Duration
	PongTimeout    time.Duration
	// Panics records a panic in a Handler callback or on the connection's own goroutines, which closes it with 1011; nil re-panics once the connection closes.
	Panics *panics.Recorder
}

func (o Options) withDefaults() Options {
	if o.ReadLimit <= 0 {
		o.ReadLimit = DefaultReadLimit
	}
	if o.SendQueue <= 0 {
		o.SendQueue = DefaultSendQueue
	}
	if o.PingInterval <= 0 {
		o.PingInterval = DefaultPingInterval
	}
	if o.PongTimeout <= 0 {
		o.PongTimeout = DefaultPongTimeout
	}
	return o
}

// Serve upgrades the request and runs the connection until it closes.
func Serve[S any](w http.ResponseWriter, r *http.Request, h Handler[S], opts Options) {
	opts = opts.withDefaults()
	names := make([]string, 0, len(opts.Subprotocols))
	for _, sp := range opts.Subprotocols {
		names = append(names, sp.Name)
	}
	hj := &hijackKeeper{ResponseWriter: w}
	ws, err := websocket.Accept(hj, r, &websocket.AcceptOptions{OriginPatterns: originPatterns(opts.AllowedOrigins), Subprotocols: names})
	if err != nil {
		return // Accept has written the HTTP error
	}
	codec := chosen(opts.Subprotocols, ws.Subprotocol())
	if codec == nil {
		_ = ws.Close(websocket.StatusCode(CloseNoSubprotocol), "ask for a subprotocol: "+strings.Join(names, ", "))
		return
	}
	ws.SetReadLimit(opts.ReadLimit)
	frame := websocket.MessageText
	if codec.Binary() {
		frame = websocket.MessageBinary
	}
	c := &conn{id: uuidv7.NewString(), ws: ws, raw: hj.conn, codec: codec, frame: frame, send: make(chan Message, opts.SendQueue), closed: make(chan struct{})}
	run(c, r.Context(), h, opts)
}

// hijackKeeper keeps the connection the upgrade takes over, so a goodbye past its deadline can drop it.
type hijackKeeper struct {
	http.ResponseWriter
	conn net.Conn
}

// Hijack takes the connection over and keeps it.
func (h *hijackKeeper) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, brw, err := http.NewResponseController(h.ResponseWriter).Hijack()
	h.conn = c
	return c, brw, err
}

// WriteHeaderNow sends the 101 before the hijack on a writer that buffers it, as Gin's does.
func (h *hijackKeeper) WriteHeaderNow() {
	if w, ok := h.ResponseWriter.(interface{ WriteHeaderNow() }); ok {
		w.WriteHeaderNow()
	}
}

// Unwrap is the writer it wraps.
func (h *hijackKeeper) Unwrap() http.ResponseWriter { return h.ResponseWriter }

func chosen(subprotocols []Subprotocol, name string) Codec {
	for _, sp := range subprotocols {
		if name != "" && strings.EqualFold(sp.Name, name) {
			return sp.Codec
		}
	}
	return nil
}

// originPatterns keeps the scheme so coder/websocket matches scheme://host, not just the host.
func originPatterns(origins []string) []string {
	out := make([]string, 0, len(origins))
	for _, o := range origins {
		if u, err := url.Parse(o); err == nil && u.Scheme != "" && u.Host != "" {
			out = append(out, u.Scheme+"://"+u.Host)
		}
	}
	return out
}

type conn struct {
	id    string
	ws    *websocket.Conn
	raw   net.Conn // under ws: closing it ends whatever ws is waiting on
	codec Codec
	frame websocket.MessageType
	send  chan Message // encoded by the writer, so a sender never waits on the codec

	once     sync.Once
	closed   chan struct{}
	closeErr *CloseError // set by Close; nil when the peer closed
}

// ID is the connection's random identifier.
func (c *conn) ID() string { return c.id }

// Send enqueues m; a full queue closes the connection.
func (c *conn) Send(m Message) error {
	select {
	case <-c.closed:
		return ErrClosed
	default:
	}
	select {
	case c.send <- m:
		return nil
	case <-c.closed:
		return ErrClosed
	default:
		c.Close(CloseSlowConsumer, "send queue full")
		return ErrQueueFull
	}
}

// Close records the reason and wakes the run loop.
func (c *conn) Close(code int, reason string) { c.closeWith(&CloseError{Code: code, Reason: reason}) }

func (c *conn) closeWith(ce *CloseError) {
	c.once.Do(func() {
		c.closeErr = ce
		close(c.closed)
	})
}

func (c *conn) closedByPeer() {
	c.once.Do(func() { close(c.closed) })
}

type frame struct {
	data []byte
	err  error
}

func run[S any](c *conn, parentCtx context.Context, h Handler[S], opts Options) {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	var wg sync.WaitGroup
	// A panic on the reader, the writer, the pinger or the last flush closes the connection with 1011, as a callback's does, and is recorded once they have all stopped.
	var crashed [4]*panics.Error
	guard := func(slot **panics.Error, fn func()) func() {
		return func() {
			defer func() {
				if p := recover(); p != nil {
					*slot = panics.Of(p)
					c.Close(CloseInternal, "internal error")
				}
			}()
			fn()
		}
	}

	// dispatch reads until closed, so every frame reaches it, the last error too, even when a drop during the upgrade cancelled the request's context.
	frames := make(chan frame)
	wg.Go(guard(&crashed[0], func() {
		for {
			_, data, err := c.ws.Read(ctx)
			select {
			case frames <- frame{data: data, err: err}:
			case <-c.closed:
				return
			}
			if err != nil {
				return
			}
		}
	}))

	// Writer: the only socket writer while the connection is open.
	writerDone := make(chan struct{})
	wg.Go(guard(&crashed[1], func() {
		defer close(writerDone)
		for {
			select {
			case m := <-c.send:
				if ce := c.write(ctx, m); ce != nil {
					c.closeWith(ce)
					return
				}
			case <-c.closed:
				return
			}
		}
	}))

	wg.Go(guard(&crashed[2], func() {
		t := time.NewTicker(opts.PingInterval)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				pctx, pcancel := context.WithTimeout(ctx, opts.PongTimeout)
				err := c.ws.Ping(pctx)
				pcancel()
				if err != nil {
					select {
					case <-c.closed:
					default:
						c.Close(CloseTimeout, "no pong")
					}
					return
				}
			case <-c.closed:
				return
			}
		}
	}))

	state, failure, readErr := callbacks(c, h, frames)

	// A peer that stopped reading never answers the close, so the goodbye has one deadline; then the socket is dropped.
	if c.closeErr != nil {
		stop := time.AfterFunc(goodbyeTimeout, func() {
			if opts.Panics == nil {
				_ = c.raw.Close()
				return
			}
			opts.Panics.Do(parentCtx, panics.FlowWS, func() { _ = c.raw.Close() }, "conn", c.id)
		})
		if c.closeErr.Code != CloseSlowConsumer && c.closeErr.Code != CloseTimeout {
			// What the handler queued before closing (a Kicked message) goes ahead of the close frame; it encodes as the writer did, so it may panic too.
			<-writerDone
			guard(&crashed[3], func() { c.flush(ctx) })()
		}
		_ = c.ws.Close(websocket.StatusCode(c.closeErr.Code), c.closeErr.Reason)
		stop.Stop()
	}
	cancel()
	_ = c.ws.CloseNow()
	wg.Wait()
	if failure != nil && opts.Panics != nil {
		opts.Panics.Record(parentCtx, panics.FlowWS, failure, "conn", c.id)
	}
	for _, e := range crashed {
		switch {
		case e == nil:
		case opts.Panics != nil:
			opts.Panics.Record(parentCtx, panics.FlowWS, e, "conn", c.id)
		case failure == nil:
			failure = e
		}
	}
	if late := onClose(c, h, state, closeReason(c.closeErr, readErr)); late != nil {
		if opts.Panics != nil {
			opts.Panics.Record(parentCtx, panics.FlowWS, late, "conn", c.id)
		} else if failure == nil {
			failure = late
		}
	}
	if failure != nil && opts.Panics == nil {
		panic(failure)
	}
}

// callbacks runs OnOpen, then OnMessage for each frame; a panic in either closes the connection with 1011.
func callbacks[S any](c *conn, h Handler[S], frames <-chan frame) (state S, failure *panics.Error, readErr error) {
	defer func() {
		if p := recover(); p != nil {
			failure = panics.Of(p)
			c.Close(CloseInternal, "internal error")
		}
	}()
	state = h.OnOpen(c)
	return state, nil, dispatch(c, h, state, frames)
}

func onClose[S any](c *conn, h Handler[S], state S, err error) (failure *panics.Error) {
	defer func() {
		if p := recover(); p != nil {
			failure = panics.Of(p)
		}
	}()
	h.OnClose(c, state, err)
	return nil
}

// dispatch returns the read error if the peer ended the connection, else nil.
func dispatch[S any](c *conn, h Handler[S], state S, frames <-chan frame) error {
	for {
		select {
		case <-c.closed:
			return nil
		case f := <-frames:
			if f.err != nil {
				c.closedByPeer()
				return f.err
			}
			m, err := c.codec.Decode(f.data)
			if err != nil {
				c.Close(CloseBadMessage, "bad message")
				return nil
			}
			h.OnMessage(c, state, m)
		}
	}
}

func closeReason(ours *CloseError, readErr error) error {
	if ours != nil {
		return ours
	}
	if ce, ok := errors.AsType[websocket.CloseError](readErr); ok {
		return &PeerClose{Code: int(ce.Code), Reason: ce.Reason}
	}
	return readErr
}

// flush writes whatever is still queued, after the writer has stopped.
func (c *conn) flush(ctx context.Context) {
	for {
		select {
		case m := <-c.send:
			if c.write(ctx, m) != nil {
				return
			}
		default:
			return
		}
	}
}

// write encodes m and writes it as one frame; a failure is how the connection closes.
func (c *conn) write(ctx context.Context, m Message) *CloseError {
	b, err := c.codec.Encode(m)
	if err != nil {
		return &CloseError{Code: CloseInternal, Reason: "internal error", Err: fmt.Errorf("%w: %s: %w", ErrEncode, m.Type, err)}
	}
	wctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	if err := c.ws.Write(wctx, c.frame, b); err != nil {
		return &CloseError{Code: CloseInternal, Reason: "write failed", Err: err}
	}
	return nil
}
