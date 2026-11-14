package hub

import (
	"context"
	"errors"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/pkg/panics"
)

// The resubscribe backoff, doubling from minBackoff to maxBackoff with ±50% jitter: no network sits between the two.
const (
	minBackoff = 10 * time.Millisecond
	maxBackoff = time.Second
)

// errClosed ends a followed subscription the producer closed: its queue filled, or a newer one under its ID replaced it.
var errClosed = errors.New("subscription closed")

// FollowOptions configure Follow.
type FollowOptions struct {
	// OnBreak is told why each subscription ended, unless ctx ended it.
	OnBreak func(err error)
	// Panics records a panic in sync or handle, which breaks the subscription; nil only reports it to OnBreak.
	Panics *panics.Recorder
}

// Follow keeps a subscription open until ctx ends, subscribing again after each break. Each time, subscribe opens one, then sync, if set, hands the consumer what comes before its events; handle runs on Follow's goroutine. A failure or a panic in sync breaks the subscription and closes it.
func Follow[T any](ctx context.Context, subscribe func() *Sub[T], sync func(ctx context.Context) error, handle func(T), opts FollowOptions) {
	b := &backoff.ExponentialBackOff{
		InitialInterval:     minBackoff,
		RandomizationFactor: backoff.DefaultRandomizationFactor,
		Multiplier:          2,
		MaxInterval:         maxBackoff,
	}
	for {
		start := time.Now()
		err := follow(ctx, subscribe, sync, handle)
		if ctx.Err() != nil {
			return
		}
		if e, ok := errors.AsType[*panics.Error](err); ok && opts.Panics != nil {
			opts.Panics.Record(ctx, panics.FlowStream, e)
		}
		if opts.OnBreak != nil {
			opts.OnBreak(err)
		}
		if time.Since(start) > maxBackoff {
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

func follow[T any](ctx context.Context, subscribe func() *Sub[T], sync func(ctx context.Context) error, handle func(T)) (err error) {
	defer panics.Capture(&err)
	sub := subscribe()
	defer sub.Close() // ends a subscription a failure or a panic left open
	if sync != nil {
		if err := sync(ctx); err != nil {
			return err
		}
	}
	for {
		select {
		case v := <-sub.C():
			handle(v)
		case <-sub.Done():
			return errClosed
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
