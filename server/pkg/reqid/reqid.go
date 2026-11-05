// Package reqid carries a request's ID in its context, from the gateway that mints it to every call and log line.
package reqid

import "context"

type key struct{}

// With returns ctx carrying id.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, key{}, id)
}

// From is the request ID in ctx, or empty.
func From(ctx context.Context) string {
	id, _ := ctx.Value(key{}).(string)
	return id
}
