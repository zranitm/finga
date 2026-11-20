package instance

import (
	"context"
	"fmt"
	"time"

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

// itemCall runs call inside callFor; one the item module doesn't answer in time is unavailable, which the gateway answers as such, not as its own deadline, a fault.
func itemCall[T any](ctx context.Context, what string, call func(context.Context) (T, error)) (T, error) {
	return transient.Within(ctx, callFor, "the item module, asked for "+what, call)
}
