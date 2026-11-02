package grpcx

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v5"
	"google.golang.org/grpc"

	"finga/pkg/panics"
)

// FollowOptions configure Follow. Zero values take the defaults.
type FollowOptions struct {
	// OnBreak is told why each subscription ended, unless ctx ended it.
	OnBreak func(err error)
	// MinBackoff and MaxBackoff bound the jittered resubscribe backoff (defaults 100 ms, 5 s).
	MinBackoff, MaxBackoff time.Duration
	// Panics records a panic in handle, which breaks the subscription like a stream error; nil only reports it to OnBreak.
	Panics *panics.Recorder
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
		if e, ok := errors.AsType[*panics.Error](err); ok && opts.Panics != nil {
			opts.Panics.Record(ctx, panics.FlowStream, e)
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

func follow[T any](ctx context.Context, open func(ctx context.Context) (grpc.ServerStreamingClient[T], error), handle func(*T)) (delivered bool, err error) {
	defer panics.Capture(&err)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // ends a stream a panic left open
	stream, err := open(ctx)
	if err != nil {
		return false, err
	}
	for {
		m, err := stream.Recv()
		if err != nil {
			return delivered, err
		}
		delivered = true
		handle(m)
	}
}
