package push

import (
	"context"
	"time"

	"github.com/google/uuid"

	"finga/services/gateway/internal/model"
)

// sessionEnded ends the player session on a login session that ended, or on an account's:
// logged_in_elsewhere on a new login, else session_expired.
func (g *Gateway) sessionEnded(ev model.SessionEnd) {
	switch {
	case !ev.Session.IsZero():
		if s := g.sessions.BySession(ev.Session); s != nil {
			s.End(model.EndSessionExpired)
		}
	case !ev.Account.IsZero():
		s := g.sessions.ByAccount(ev.Account)
		if s == nil {
			return
		}
		why := model.EndSessionExpired
		if ev.NewLogin {
			why = model.EndLoggedInElsewhere
		}
		s.End(why)
	}
}

func (g *Gateway) checkSessionsEvery(ctx context.Context) {
	ticker := time.NewTicker(g.deps.SessionCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.checkSessions(ctx)
		}
	}
}

// checkSessions ends every player session whose login session no longer holds, which a broken stream missed.
func (g *Gateway) checkSessions(ctx context.Context) {
	for _, s := range g.sessions.LoggedIn() {
		if err := g.deps.Sessions.Check(ctx, uuid.UUID(s.LoginSession())); err != nil {
			s.End(model.EndSessionExpired)
		}
	}
}
