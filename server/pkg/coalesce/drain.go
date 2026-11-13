package coalesce

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v5"
)

// Outcome is what a consumer made of a value it took.
type Outcome int

// Outcomes a consumer's handle reports.
const (
	Handled Outcome = iota // done with it; the next value is taken at once
	Dropped                // given up on; the next value is taken at once
	Retry                  // put back behind the others, the next take waiting a pause first
)

// Drain hands each value to handle in turn until ctx ends; a Retry pauses for wait's next backoff, which a Handled resets. A panic in handle goes on up.
func (q *Queue[K, V]) Drain(ctx context.Context, wait backoff.BackOff, handle func(V) Outcome) {
	wait.Reset()
	for ctx.Err() == nil {
		v, ok := q.Take()
		if !ok {
			select {
			case <-q.Ready():
			case <-ctx.Done():
			}
			continue
		}
		out := Dropped // a panic in handle drops v but gives its key back, so a consumer that restarts still takes the key's next value
		func() {
			defer func() { q.Done(v, out == Retry) }()
			out = handle(v)
		}()
		switch out {
		case Handled:
			wait.Reset()
		case Retry:
			pause(ctx, wait.NextBackOff())
		}
	}
}

// pause waits d, or until ctx ends.
func pause(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}
