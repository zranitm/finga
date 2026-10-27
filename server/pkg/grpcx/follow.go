package grpcx

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

// FollowOptions configure Follow. Zero values take the defaults.
type FollowOptions struct {
	// OnBreak is told why each subscription ended (never when the context
	// ended it), before Follow waits and subscribes again.
	OnBreak func(err error)
	// MinBackoff and MaxBackoff bound the wait before subscribing again; it
	// doubles after each break that delivered nothing and resets after one
	// that delivered a message. Defaults 100 ms and 5 s.
	MinBackoff, MaxBackoff time.Duration
}

// Follow keeps a server stream open until ctx ends: it opens it, hands
// each message to handle (on Follow's goroutine, in order), and when the
// stream breaks it reports why, waits, and opens it again.
func Follow[T any](ctx context.Context, open func(ctx context.Context) (grpc.ServerStreamingClient[T], error), handle func(*T), opts FollowOptions) {
	if opts.MinBackoff <= 0 {
		opts.MinBackoff = 100 * time.Millisecond
	}
	if opts.MaxBackoff < opts.MinBackoff {
		opts.MaxBackoff = max(5*time.Second, opts.MinBackoff)
	}
	wait := opts.MinBackoff
	for {
		delivered, err := follow(ctx, open, handle)
		if ctx.Err() != nil {
			return
		}
		if opts.OnBreak != nil {
			opts.OnBreak(err)
		}
		if delivered {
			wait = opts.MinBackoff
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
		if !delivered {
			wait = min(wait*2, opts.MaxBackoff)
		}
	}
}

// follow runs one subscription until it breaks.
func follow[T any](ctx context.Context, open func(ctx context.Context) (grpc.ServerStreamingClient[T], error), handle func(*T)) (bool, error) {
	stream, err := open(ctx)
	if err != nil {
		return false, err
	}
	delivered := false
	for {
		m, err := stream.Recv()
		if err != nil {
			return delivered, err
		}
		delivered = true
		handle(m)
	}
}
