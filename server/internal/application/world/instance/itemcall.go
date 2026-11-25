package instance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/cenkalti/backoff/v5"

	"finga/pkg/transient"
)

// callFor is all an item-module call made for a player's command may take, every try included: inside the 5 s the gateway and the client give a command, with room for the queues and ticks either side, so the command hears why.
const callFor = 3500 * time.Millisecond

// answerRoom is how long before a command's deadline its wait for an item call gives up as unavailable: callFor counts from when a worker took the call, after the queues and ticks before it, which a slow box can stretch past the rest of the 5 s.
const answerRoom = 500 * time.Millisecond

// answering is ctx ended answerRoom before its deadline, with what unavailable as the cause; ctx itself when it has no deadline.
func answering(ctx context.Context, what string) (context.Context, context.CancelFunc) {
	end, ok := ctx.Deadline()
	if !ok {
		return ctx, func() {}
	}
	return context.WithDeadlineCause(ctx, end.Add(-answerRoom), fmt.Errorf("%w: the item module, asked for %s, didn't answer in time", transient.ErrUnavailable, what))
}

// A worker's call that's handed in, given or granted once however often it's tried (a hand-in, a trainer's gift, a feat's grant) is tried again after a timeout or an outage; a refusal is final. Every try fits in callFor, so the outcome reaches the request.
const (
	tryTimeout = 1500 * time.Millisecond // one try
	retryFirst = 200 * time.Millisecond
	retryMax   = time.Second
)

// retried tries call until the item module answers or refuses, or callFor passes, each try within tryTimeout and backing off between; the log hears each try that failed, naming what and who. It gives the tries made.
func retried[T any](ctx context.Context, log *slog.Logger, what string, who []any, call func(context.Context) (T, error)) (T, int, error) {
	ctx, cancel := context.WithTimeoutCause(ctx, callFor, fmt.Errorf("%w: the item module didn't answer the %s in time", transient.ErrUnavailable, what))
	defer cancel()
	tries := 0
	wait := &backoff.ExponentialBackOff{InitialInterval: retryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: retryMax}
	out, err := backoff.Retry(ctx, func() (T, error) {
		tries++
		tryCtx, cancelTry := context.WithTimeout(ctx, tryTimeout)
		defer cancelTry()
		out, err := call(tryCtx)
		if err != nil && !errors.Is(err, transient.ErrUnavailable) {
			return out, backoff.Permanent(err)
		}
		return out, err
	}, backoff.WithBackOff(wait), backoff.WithMaxElapsedTime(0), backoff.WithNotify(func(err error, next time.Duration) {
		log.WarnContext(ctx, "item call try failed; trying again", append(who, "call", what, "try", tries, "next_in", next, "err", err)...)
	}))
	return out, tries, err
}

// itemCall runs call inside callFor; one the item module doesn't answer in time is unavailable, which the gateway answers as such, not as its own deadline, a fault.
func itemCall[T any](ctx context.Context, what string, call func(context.Context) (T, error)) (T, error) {
	return transient.Within(ctx, callFor, "the item module, asked for "+what, call)
}
