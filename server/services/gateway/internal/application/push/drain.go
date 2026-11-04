package push

import (
	"context"
	"time"

	"finga/services/gateway/internal/model"
)

const drainPoll = 10 * time.Millisecond

// Drain refuses new connections, ends open ones with server_restart and waits until they close or ctx ends.
func (g *Gateway) Drain(ctx context.Context) {
	for _, s := range g.sessions.Drain() {
		s.End(model.EndServerRestart)
	}
	t := time.NewTicker(drainPoll)
	defer t.Stop()
	for {
		open := g.sessions.Len()
		if open == 0 {
			return
		}
		select {
		case <-ctx.Done():
			g.deps.Logger.WarnContext(ctx, "drain ended with connections open", "open", open)
			return
		case <-t.C:
		}
	}
}
