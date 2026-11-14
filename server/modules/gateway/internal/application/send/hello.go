package send

import (
	"context"
	"fmt"

	"finga/internal/kernel"
	"finga/modules/gateway/internal/application/session"
	"finga/modules/gateway/internal/model"
)

// Open starts the session of the connection named connID, which the handler passes to every use case.
func (g *Gateway) Open(connID string, o session.Outbox) *session.Session {
	return g.sessions.Open(connID, o)
}

// Hello joins the connection's character and sends Welcome; model.ErrHelloRejected means a bad token or character.
func (g *Gateway) Hello(ctx context.Context, s *session.Session, token string, charID kernel.CharacterID) error {
	ctx, cancel := context.WithTimeout(ctx, g.opts.JoinTimeout)
	defer cancel()
	p, err := g.deps.Sessions.Validate(ctx, token)
	if err != nil {
		if model.PublicOf(err) == model.Unauthenticated {
			return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
		}
		g.deps.Logger.WarnContext(ctx, "token check failed", "err", err)
		return fmt.Errorf("validate: %w", err) // the account service is down: the client keeps its token and retries
	}

	join, err := g.sessions.Join(ctx, s, p.AccountID, p.SessionID, charID)
	if err != nil {
		if model.PublicOf(err) == model.CharacterNotFound {
			return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
		}
		g.deps.Logger.ErrorContext(ctx, "join failed", "character_id", charID, "err", err)
		return fmt.Errorf("join: %w", err)
	}
	defer join.Release() // a failure or a panic before Finish gives the join up
	welcome, err := g.sessions.Welcome(ctx, charID, join.Joined)
	if err != nil {
		g.deps.Logger.ErrorContext(ctx, "welcome failed", "character_id", charID, "err", err)
		return fmt.Errorf("welcome: %w", err)
	}
	// Last, so a Hello that fails before here leaves the client's token as it was.
	welcome.Token, err = g.rotate(ctx, p.SessionID)
	if err != nil {
		if model.PublicOf(err) == model.Unauthenticated {
			return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
		}
		g.deps.Logger.ErrorContext(ctx, "token rotation failed", "session_id", p.SessionID, "err", err)
		return fmt.Errorf("rotate: %w", err)
	}

	// The newest connection wins.
	for _, older := range join.Finish(model.PlayerOf(charID, welcome.Character), welcome) {
		older.End(model.EndLoggedInElsewhere)
	}
	g.sessions.Register(s)
	g.deps.Observer.OnlineChanged(1)
	g.deps.Logger.InfoContext(ctx, "ws joined", "conn", s.ConnID(), "character_id", charID, "map_id", join.Joined.MapID)
	return nil
}

// rotate has its own budget: a join that retried may have spent the Hello's.
func (g *Gateway) rotate(ctx context.Context, sessionID kernel.SessionID) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rotateTimeout)
	defer cancel()
	return g.deps.Sessions.Rotate(ctx, sessionID)
}

// Closed ends a session once its connection closed and none of its commands still runs.
func (g *Gateway) Closed(s *session.Session) {
	if g.sessions.Close(s) {
		g.deps.Observer.OnlineChanged(-1)
	}
}
