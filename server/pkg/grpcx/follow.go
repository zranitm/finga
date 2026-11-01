package grpcx

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v5"
	"google.golang.org/grpc"
)

// FollowOptions configure Follow. Zero values take the defaults.
type FollowOptions struct {
	// OnBreak is told why each subscription ended, unless ctx ended it.
	OnBreak func(err error)
	// MinBackoff and MaxBackoff bound the jittered resubscribe backoff (defaults 100 ms, 5 s).
	MinBackoff, MaxBackoff time.Duration
}

// Follow keeps a server stream open until ctx ends; handle runs on Follow's goroutine.
func Follow[T any](ctx context.Context, open func(ctx context.Context) (grpc.ServerStreamingClient[T], error), handle func(*T), opts FollowOptions) {
	if opts.MinBackoff <= 0 {
		opts.MinBackoff = 100 * time.Millisecond
	}
	if opts.MaxBackoff < opts.MinBackoff {
		opts.MaxBackoff = max(5*time.Second, opts.MinBackoff)
	}
	b := &backoff.ExponentialBackOff{
		InitialInterval:     opts.MinBackoff,
		RandomizationFactor: backoff.DefaultRandomizationFactor,
		Multiplier:          2,
		MaxInterval:         opts.MaxBackoff,
	}
	for {
		delivered, err := follow(ctx, open, handle)
		if ctx.Err() != nil {
			return
		}
		if opts.OnBreak != nil {
			opts.OnBreak(err)
		}
		if delivered {
			b.Reset()
		}
		t := time.NewTimer(b.NextBackOff())
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

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
