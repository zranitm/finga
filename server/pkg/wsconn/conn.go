package wsconn

import (
	"context"
	"errors"
	"fmt"
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
	CloseNormal        = int(websocket.StatusNormalClosure)
	CloseGoingAway     = int(websocket.StatusGoingAway)
	CloseTooBig        = int(websocket.StatusMessageTooBig)
	CloseInternal      = int(websocket.StatusInternalError)
	CloseNoSubprotocol = int(websocket.StatusPolicyViolation) // the client asked for none of the server's subprotocols
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
)

// The connection's errors.
var (
	ErrQueueFull = errors.New("wsconn: send queue full")
	ErrClosed    = errors.New("wsconn: connection closed")
)

// CloseError is why we closed a connection; a peer's normal closure gives OnClose nil.
type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string { return fmt.Sprintf("wsconn: closed %d: %s", e.Code, e.Reason) }

// Conn is a live connection as the handler sees it.
type Conn interface {
	ID() string
	// Send enqueues without blocking; a full queue closes the connection.
	Send(Message) error
	// Close ends the connection; the first call wins.
	Close(code int, reason string)
}

// Handler callbacks run on the read goroutine one at a time; OnClose runs last, exactly once.
type Handler interface {
	OnOpen(Conn)
	OnMessage(Conn, Message)
	OnClose(Conn, error)
}

// Subprotocol is a codec a client asks for by name when it connects.
type Subprotocol struct {
	Name  string
	Codec Codec
}

// Options tune Serve. Zero values take the defaults.
type Options struct {
	// Codec serves every connection when Subprotocols is empty.
	Codec Codec
	// Subprotocols are the codecs a client picks from, in the server's order of preference.
	Subprotocols   []Subprotocol
	AllowedOrigins []string // full origins, like http://localhost:5173; empty: same host only
	ReadLimit      int64
	SendQueue      int
	PingInterval   time.Duration
	PongTimeout    time.Duration
	// Panics records a panic in a Handler callback, which closes the connection with 1011;
	// nil passes the panic on to the HTTP server once the connection is closed.
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
func Serve(w http.ResponseWriter, r *http.Request, h Handler, opts Options) {
	opts = opts.withDefaults()
	names := make([]string, 0, len(opts.Subprotocols))
	for _, sp := range opts.Subprotocols {
		names = append(names, sp.Name)
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: originPatterns(opts.AllowedOrigins), Subprotocols: names})
	if err != nil {
		return // Accept has written the HTTP error
	}
	codec := opts.Codec
	if len(names) > 0 {
		codec = chosen(opts.Subprotocols, ws.Subprotocol())
		if codec == nil {
			_ = ws.Close(websocket.StatusCode(CloseNoSubprotocol), "ask for a subprotocol: "+strings.Join(names, ", "))
			return
		}
	}
	ws.SetReadLimit(opts.ReadLimit)
	frame := websocket.MessageText
	if codec.Binary() {
		frame = websocket.MessageBinary
	}
	c := &conn{id: uuidv7.NewString(), ws: ws, codec: codec, frame: frame, send: make(chan []byte, opts.SendQueue), closed: make(chan struct{})}
	c.run(r.Context(), h, opts)
}

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
	codec Codec
	frame websocket.MessageType
	send  chan []byte

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
	b, err := c.codec.Encode(m)
	if err != nil {
		return err
	}
	select {
	case c.send <- b:
		return nil
	case <-c.closed:
		return ErrClosed
	default:
		c.Close(CloseSlowConsumer, "send queue full")
		return ErrQueueFull
	}
}

// Close records the reason and wakes the run loop.
func (c *conn) Close(code int, reason string) {
	c.once.Do(func() {
		c.closeErr = &CloseError{Code: code, Reason: reason}
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

func (c *conn) run(parent context.Context, h Handler, opts Options) {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	var wg sync.WaitGroup

	frames := make(chan frame)
	wg.Go(func() {
		for {
			_, data, err := c.ws.Read(ctx)
			select {
			case frames <- frame{data: data, err: err}:
			case <-ctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	})

	// Writer: the only socket writer while the connection is open.
	writerDone := make(chan struct{})
	wg.Go(func() {
		defer close(writerDone)
		for {
			select {
			case b := <-c.send:
				wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
				err := c.ws.Write(wctx, c.frame, b)
				wcancel()
				if err != nil {
					c.Close(CloseInternal, "write failed")
					return
				}
			case <-c.closed:
				return
			}
		}
	})

	wg.Go(func() {
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
	})

	failure, readErr := c.callbacks(h, frames)

	// A peer that stopped reading never answers the close handshake, so drop it at once.
	if c.closeErr != nil {
		switch c.closeErr.Code {
		case CloseSlowConsumer, CloseTimeout:
			_ = c.ws.CloseNow()
		default:
			// Flush what the handler queued before closing (a Kicked message) ahead of the close frame.
			<-writerDone
			c.flush(ctx)
			_ = c.ws.Close(websocket.StatusCode(c.closeErr.Code), c.closeErr.Reason) //nolint:gosec // close codes are small
		}
	}
	cancel()
	_ = c.ws.CloseNow()
	wg.Wait()
	if failure != nil && opts.Panics != nil {
		opts.Panics.Record(parent, panics.FlowWS, failure, "conn", c.id)
	}
	if late := onClose(c, h, closeReason(c.closeErr, readErr)); late != nil {
		if opts.Panics != nil {
			opts.Panics.Record(parent, panics.FlowWS, late, "conn", c.id)
		} else if failure == nil {
			failure = late
		}
	}
	if failure != nil && opts.Panics == nil {
		panic(failure)
	}
}

// callbacks runs OnOpen, then OnMessage for each frame; a panic in either closes the connection with 1011.
func (c *conn) callbacks(h Handler, frames <-chan frame) (failure *panics.Error, readErr error) {
	defer func() {
		if p := recover(); p != nil {
			failure = panics.Of(p)
			c.Close(CloseInternal, "internal error")
		}
	}()
	h.OnOpen(c)
	return nil, c.dispatch(h, frames)
}

func onClose(c *conn, h Handler, err error) (failure *panics.Error) {
	defer func() {
		if p := recover(); p != nil {
			failure = panics.Of(p)
		}
	}()
	h.OnClose(c, err)
	return nil
}

// dispatch returns the read error if the peer ended the connection, else nil.
func (c *conn) dispatch(h Handler, frames <-chan frame) error {
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
			h.OnMessage(c, m)
		}
	}
}

func closeReason(ours *CloseError, readErr error) error {
	if ours != nil {
		return ours
	}
	switch websocket.CloseStatus(readErr) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		return nil
	}
	if readErr == nil {
		return nil
	}
	return readErr
}

// flush writes whatever is still queued, after the writer has stopped.
func (c *conn) flush(ctx context.Context) {
	for {
		select {
		case b := <-c.send:
			wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Write(wctx, c.frame, b)
			wcancel()
			if err != nil {
				return
			}
		default:
			return
		}
	}
}
