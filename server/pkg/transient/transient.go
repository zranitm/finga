// Package transient names a failure a later try may clear, so a handler can map it without importing what failed.
package transient

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ErrUnavailable is a dependency busy or out of reach: a database past its lock or statement timeout, refusing connections, or restarting.
var ErrUnavailable = errors.New("busy or out of reach")

// Within runs call with d to answer in. One that doesn't fails as unavailable, what naming the call, rather than with its caller's deadline, which a caller would count as its own fault.
func Within[T any](ctx context.Context, d time.Duration, what string, call func(context.Context) (T, error)) (T, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, d, fmt.Errorf("%w: %s didn't answer in time", ErrUnavailable, what))
	defer cancel()
	out, err := call(ctx)
	if err != nil && ctx.Err() != nil {
		err = context.Cause(ctx)
	}
	return out, err
}
