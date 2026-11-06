package push

import (
	"context"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"finga/services/gateway/internal/model"
)

// sessionEnded ends the player session on a login session that ended, or on an account's:
// logged_in_elsewhere on a new login, else session_expired.
func (g *Gateway) sessionEnded(ev model.SessionEnd) {
	switch ev := ev.(type) {
	case model.SessionRevoked:
		if s := g.sessions.BySession(ev.Session); s != nil {
			s.End(model.EndSessionExpired)
		}
	case model.AccountRevoked:
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
	ticker := time.NewTicker(g.opts.SessionCheckInterval)
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

// The session check's limits: a slow account service delays the round, never stops it.
const (
	checkTimeout = 5 * time.Second
	checksAtOnce = 16
)

// checkSessions ends every player session whose login session no longer holds, which a broken stream
// missed; any other failure (the account service down or slow) is logged and ends nothing.
func (g *Gateway) checkSessions(ctx context.Context) {
	var round errgroup.Group
	round.SetLimit(checksAtOnce)
	for _, s := range g.sessions.LoggedIn() {
		round.Go(func() error {
			ctx, cancel := context.WithTimeout(ctx, checkTimeout)
			defer cancel()
			err := g.deps.Sessions.Check(ctx, uuid.UUID(s.LoginSession()))
			switch {
			case err == nil:
			case model.PublicOf(err) == model.Unauthenticated:
				s.End(model.EndSessionExpired)
			default:
				g.deps.Logger.WarnContext(ctx, "session check failed", "session_id", s.LoginSession(), "err", err)
			}
			return nil
		})
	}
	_ = round.Wait()
}
