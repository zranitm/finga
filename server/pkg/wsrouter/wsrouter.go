// Package wsrouter routes decoded WebSocket messages by type through
// middleware chains, as an HTTP router routes requests by path: each
// message type is registered once, with its middleware and a typed
// handler, and every message runs the router's global middleware, then its
// route's, then the handler. It knows nothing of the transport or the
// encoding (pkg/wsconn decodes; the caller passes the decoded message in)
// and nothing of the application: S is the caller's connection state,
// which every handler gets.
//
// A middleware is a HandlerFunc that calls c.Next to run the rest of the
// chain and returns when it is done; code after Next runs once the rest
// has. A middleware that does not call Next stops the chain there. Next
// may also be called later, from another goroutine: a middleware can hand
// the rest of a chain to a connection's command queue, so a message is
// checked on the read loop and handled on the queue.
package wsrouter

import (
	"context"
	"fmt"
	"slices"
)

// Message is the part of a decoded message the router reads.
type Message struct {
	Type  string
	ReqID string // request and response correlation, optional
	Seq   uint32 // the client's sequence number, optional
	Data  any    // the decoded payload, of the type its route was registered with
}

// HandlerFunc is one step of a chain: a middleware, or the end of one.
type HandlerFunc[S any] func(c *Context[S])

// Context is one message on its way through its chain. A context is used
// by one chain only and never reused, so a step may keep it after
// returning (to call Next later).
type Context[S any] struct {
	Session S       // the connection's state
	Message Message // the message being handled
	// Ctx is the context the handler's calls run under: Background until a
	// middleware sets one (a deadline, the connection's cancellation).
	Ctx context.Context
	// Result and Err are what the typed handler returned, for the
	// middleware around it (to answer the client, to log).
	Result any
	Err    error

	chain []HandlerFunc[S]
	index int
}

// Next runs the rest of the chain.
func (c *Context[S]) Next() {
	c.index++
	if c.index < len(c.chain) {
		c.chain[c.index](c)
	}
}

// Router holds the routes of one kind of connection.
type Router[S any] struct {
	global  []HandlerFunc[S]
	routes  map[string][]HandlerFunc[S]
	noRoute []HandlerFunc[S]
}

// New makes a router whose every message runs middleware first.
func New[S any](middleware ...HandlerFunc[S]) *Router[S] {
	return &Router[S]{global: middleware, routes: map[string][]HandlerFunc[S]{}}
}

// Handle routes msgType through middleware to h, which gets the payload
// as a T. What h returns lands in the context's Result and Err. A message
// whose payload is not a T never reaches h: Err says so. Routing a type
// twice panics, as registering a path twice does in net/http.
func Handle[S, T any](r *Router[S], msgType string, h func(c *Context[S], in T) (any, error), middleware ...HandlerFunc[S]) {
	if _, taken := r.routes[msgType]; taken {
		panic("wsrouter: " + msgType + " is routed twice")
	}
	end := func(c *Context[S]) {
		in, ok := c.Message.Data.(T)
		if !ok {
			var want T
			c.Err = fmt.Errorf("wsrouter: %s carries %T, want %T", msgType, c.Message.Data, want)
			return
		}
		c.Result, c.Err = h(c, in)
	}
	r.routes[msgType] = append(slices.Clone(middleware), end)
}

// NoRoute sets the chain a message of an unrouted type runs after the
// global middleware; without one, such a message runs only the global
// middleware.
func (r *Router[S]) NoRoute(handlers ...HandlerFunc[S]) { r.noRoute = handlers }

// Dispatch runs m through its chain for the connection session.
func (r *Router[S]) Dispatch(session S, m Message) {
	route, ok := r.routes[m.Type]
	if !ok {
		route = r.noRoute
	}
	c := &Context[S]{Session: session, Message: m, Ctx: context.Background(), chain: append(slices.Clone(r.global), route...), index: -1}
	c.Next()
}

// Types lists the routed message types, sorted.
func (r *Router[S]) Types() []string {
	types := make([]string, 0, len(r.routes))
	for t := range r.routes {
		types = append(types, t)
	}
	slices.Sort(types)
	return types
}
