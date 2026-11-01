// Package wsrouter routes decoded WebSocket messages by type through middleware chains.
// A middleware calls c.Next to continue, possibly later on another goroutine, or skips it to stop.
package wsrouter

import (
	"context"
	"fmt"
	"slices"
)

// Message is the part of a decoded message the router reads.
type Message struct {
	Type  string
	ReqID string
	Seq   uint32
	Data  any
}

// HandlerFunc is one step of a chain: a middleware, or the end of one.
type HandlerFunc[S any] func(c *Context[S])

// Context is one message on its way through its chain; a step may keep it to call Next later.
type Context[S any] struct {
	Session S
	Message Message
	// Ctx is what the handler's calls run under: Background unless a middleware sets one.
	Ctx context.Context
	// Result and Err are the typed handler's return values, for the middleware around it.
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

// Handle routes msgType to h; a payload that isn't a T sets c.Err, and routing twice panics.
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

// NoRoute sets the chain unrouted types run after the global middleware.
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
