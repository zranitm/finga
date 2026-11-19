package instance

import (
	"context"
	"time"

	"finga/pkg/transient"
)

// callFor is all an item-module call made for a player's command may take, every try included: inside the 5 s the gateway and the client give a command, with room for the queues and ticks either side, so the command hears why.
const callFor = 3500 * time.Millisecond

// itemCall runs call inside callFor; one the item module doesn't answer in time is unavailable, which the gateway answers as such, not as its own deadline, a fault.
func itemCall[T any](ctx context.Context, what string, call func(context.Context) (T, error)) (T, error) {
	return transient.Within(ctx, callFor, "the item module, asked for "+what, call)
}
