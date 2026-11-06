package push

import (
	"context"
	"time"

	"finga/services/gateway/internal/model"
)

const drainPoll = 10 * time.Millisecond

// Drain refuses new connections, ends open ones with server_restart and waits, until ctx ends, for each to
// close and its player to leave, and for the registry's background jobs.
func (g *Gateway) Drain(ctx context.Context) {
	for _, s := range g.sessions.Drain() {
		s.End(model.EndServerRestart)
	}
	t := time.NewTicker(drainPoll)
	defer t.Stop()
	for {
		sessions, jobs := g.sessions.Pending()
		if sessions == 0 && jobs == 0 {
			return
		}
		select {
		case <-ctx.Done():
			g.deps.Logger.WarnContext(ctx, "drain ended with work open", "sessions", sessions, "jobs", jobs)
			return
		case <-t.C:
		}
	}
}
