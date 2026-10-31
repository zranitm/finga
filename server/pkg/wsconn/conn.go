package wsconn

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Close codes: the standard ones the layer uses, and application codes in
// the 4000 range for the reasons it closes connections itself.
const (
	CloseNormal       = int(websocket.StatusNormalClosure)
	CloseGoingAway    = int(websocket.StatusGoingAway)
	CloseTooBig       = int(websocket.StatusMessageTooBig)
	CloseInternal     = int(websocket.StatusInternalError)
	CloseSlowConsumer = 4001 // the send queue filled up
	CloseTimeout      = 4002 // no pong in time
	CloseKicked       = 4003 // the handler ended the session (reason says why)
	CloseBadMessage   = 4004 // an unknown or malformed message
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

// CloseError is why the layer or the handler closed a connection; OnClose
// receives it. A peer's normal closure is reported as a nil error.
type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string { return fmt.Sprintf("wsconn: closed %d: %s", e.Code, e.Reason) }

// Conn is a live connection as the handler sees it.
type Conn interface {
	ID() string
	// Send enqueues a message without blocking. A full queue closes the
	// connection (the peer is too slow) and returns ErrQueueFull.
	Send(Message) error
	// Close ends the connection with a code and reason; the first call wins.
	Close(code int, reason string)
}

// Handler receives connection events. OnOpen and OnMessage run on the
// connection's read goroutine, one message at a time; OnClose runs exactly
// once, after every other callback has returned.
type Handler interface {
	OnOpen(Conn)
	OnMessage(Conn, Message)
	OnClose(Conn, error)
}

// Options tune Serve. Zero values take the defaults.
type Options struct {
	Codec          Codec
	AllowedOrigins []string // full origins, like http://localhost:5173; empty: same host only
	ReadLimit      int64
	SendQueue      int
	PingInterval   time.Duration
	PongTimeout    time.Duration
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

// Serve upgrades the request and runs the connection until it closes: the
// read loop on this goroutine, a write loop and a ping loop beside it. A
// refused origin answers 403 and returns without calling the handler.
func Serve(w http.ResponseWriter, r *http.Request, h Handler, opts Options) {
	opts = opts.withDefaults()
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: originPatterns(opts.AllowedOrigins)})
	if err != nil {
		return // Accept has written the HTTP error
	}
	ws.SetReadLimit(opts.ReadLimit)
	c := &conn{id: newID(), ws: ws, codec: opts.Codec, send: make(chan []byte, opts.SendQueue), closed: make(chan struct{})}
	c.run(r.Context(), h, opts)
}

// originPatterns turns full origins into coder/websocket patterns, which
// match "scheme://host" when they contain a scheme.
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
	send  chan []byte

	once     sync.Once
	closed   chan struct{}
	closeErr *CloseError // set by Close; nil when the peer closed
}

// ID is the connection's random identifier.
func (c *conn) ID() string { return c.id }

// Send enqueues a message; a full queue closes the connection.
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

// Close records the reason and wakes the run loop; the first call wins.
func (c *conn) Close(code int, reason string) {
	c.once.Do(func() {
		c.closeErr = &CloseError{Code: code, Reason: reason}
		close(c.closed)
	})
}

// closedByPeer records a read error (the peer closed, or the socket died)
// without a CloseError of our own.
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

	// Reader: the only goroutine that reads from the socket.
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

	// Writer: the only goroutine that writes to the socket while the
	// connection is open.
	writerDone := make(chan struct{})
	wg.Go(func() {
		defer close(writerDone)
		for {
			select {
			case b := <-c.send:
				wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
				err := c.ws.Write(wctx, websocket.MessageText, b)
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

	// Pinger: a missed pong ends the connection.
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

	h.OnOpen(c)
	readErr := c.dispatch(h, frames)

	// Tell the peer why, when it was our decision, then tear down. A peer
	// that stopped reading (slow consumer, missed pong) would never answer
	// the close handshake, so those paths drop the socket at once.
	if c.closeErr != nil {
		switch c.closeErr.Code {
		case CloseSlowConsumer, CloseTimeout:
			_ = c.ws.CloseNow()
		default:
			// The writer has stopped (closed is closed); flush what the
			// handler queued just before closing, such as a Kicked message,
			// so the peer learns why before the close frame.
			<-writerDone
			c.flush(ctx)
			_ = c.ws.Close(websocket.StatusCode(c.closeErr.Code), c.closeErr.Reason) //nolint:gosec // close codes are small
		}
	}
	cancel()
	_ = c.ws.CloseNow()
	wg.Wait()
	h.OnClose(c, closeReason(c.closeErr, readErr))
}

// dispatch hands each frame the reader receives to h, decoded, one at a
// time, until the connection ends: closed by us (nil), by the peer (the
// read error, returned for OnClose), or by a frame that does not decode
// (nil; Close records why).
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

// closeReason is what OnClose gets: our CloseError when we closed, nil for
// a peer's normal closure, else the read error.
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

func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}

// flush writes whatever is still queued, after the writer has stopped.
func (c *conn) flush(ctx context.Context) {
	for {
		select {
		case b := <-c.send:
			wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
			err := c.ws.Write(wctx, websocket.MessageText, b)
			wcancel()
			if err != nil {
				return
			}
		default:
			return
		}
	}
}
