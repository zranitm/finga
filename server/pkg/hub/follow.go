package hub

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/pkg/panics"
)

// ErrClosed ends a followed subscription the producer closed: its queue filled, or a newer one under its ID replaced it.
var ErrClosed = errors.New("subscription closed")

// FollowOptions configure Follow. Zero values take the defaults.
type FollowOptions struct {
	// OnBreak is told why each subscription ended, unless ctx ended it.
	OnBreak func(err error)
	// MinBackoff and MaxBackoff bound the jittered resubscribe backoff (defaults 10 ms, 1 s): no network sits between the two.
	MinBackoff, MaxBackoff time.Duration
	// Panics records a panic in open or handle, which breaks the subscription; nil only reports it to OnBreak.
	Panics *panics.Recorder
}

// Follow keeps a subscription open until ctx ends, resubscribing after each break; open subscribes and hands the consumer what comes before the events, and handle runs on Follow's goroutine.
func Follow[T any](ctx context.Context, open func(ctx context.Context) (*Sub[T], error), handle func(T), opts FollowOptions) {
	if opts.MinBackoff <= 0 {
		opts.MinBackoff = 10 * time.Millisecond
	}
	if opts.MaxBackoff < opts.MinBackoff {
		opts.MaxBackoff = max(time.Second, opts.MinBackoff)
	}
	b := &backoff.ExponentialBackOff{
		InitialInterval:     opts.MinBackoff,
		RandomizationFactor: backoff.DefaultRandomizationFactor,
		Multiplier:          2,
		MaxInterval:         opts.MaxBackoff,
	}
	for {
		start := time.Now()
		err := follow(ctx, open, handle)
		if ctx.Err() != nil {
			return
		}
		if e, ok := errors.AsType[*panics.Error](err); ok && opts.Panics != nil {
			opts.Panics.Record(ctx, panics.FlowStream, e)
		}
		if opts.OnBreak != nil {
			opts.OnBreak(err)
		}
		if time.Since(start) > opts.MaxBackoff {
			b.Reset() // it held, so this is no crash loop
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

func follow[T any](ctx context.Context, open func(ctx context.Context) (*Sub[T], error), handle func(T)) (err error) {
	defer panics.Capture(&err)
	sub, err := open(ctx)
	if err != nil {
		return err
	}
	defer sub.Close() // ends a subscription a panic left open
	for {
		select {
		case v := <-sub.C():
			handle(v)
		case <-sub.Done():
			return ErrClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
