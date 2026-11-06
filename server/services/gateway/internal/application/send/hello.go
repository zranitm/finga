package send

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"finga/internal/kernel"
	"finga/services/gateway/internal/application/session"
	"finga/services/gateway/internal/model"
)

// Open starts the session of the connection named connID, which the handler passes to every use case.
func (g *Gateway) Open(connID string, o session.Outbox) *session.Session {
	return g.sessions.Open(connID, o)
}

// Hello joins the connection's character and sends Welcome; model.ErrHelloRejected means a bad token or character.
func (g *Gateway) Hello(ctx context.Context, s *session.Session, token string, charID kernel.CharacterID) error {
	ctx, cancel := context.WithTimeout(ctx, g.deps.JoinTimeout)
	defer cancel()
	p, err := g.deps.Sessions.Validate(ctx, token)
	if err != nil {
		if model.PublicOf(err) == model.Unauthenticated {
			return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
		}
		g.deps.Logger.WarnContext(ctx, "token check failed", "err", err)
		return fmt.Errorf("validate: %w", err) // the account service is down: the client keeps its token and retries
	}

	join, err := g.sessions.Join(ctx, s, kernel.AccountID(p.AccountID), kernel.SessionID(p.SessionID), charID)
	if err != nil {
		if model.PublicOf(err) == model.CharacterNotFound {
			return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
		}
		g.deps.Logger.ErrorContext(ctx, "join failed", "character_id", charID, "err", err)
		return fmt.Errorf("join: %w", err)
	}
	defer join.Release() // a failure or a panic before Finish gives the join up
	c := join.Joined.Character
	welcome := model.Welcome{Character: c, MapID: join.Joined.MapID, ServerTime: time.Now(), TickRateHz: join.Joined.TickRateHz}
	items, err := g.deps.Items.Inventory(ctx, charID)
	if err != nil {
		g.deps.Logger.ErrorContext(ctx, "inventory failed", "character_id", charID, "err", err)
		return fmt.Errorf("inventory: %w", err)
	}
	welcome.Inventory = items
	party, err := g.deps.Social.PartyOf(ctx, charID)
	if err != nil {
		g.deps.Logger.WarnContext(ctx, "party lookup failed", "character_id", charID, "err", err) // a social failure must not block the join
	}
	welcome.Party = party
	// Last, so a Hello that fails before here leaves the client's token as it was; the one it sent works
	// once more for a minute, and Welcome's from now on.
	welcome.Token, err = g.rotate(ctx, p.SessionID)
	if err != nil {
		if model.PublicOf(err) == model.Unauthenticated {
			return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
		}
		g.deps.Logger.ErrorContext(ctx, "token rotation failed", "session_id", p.SessionID, "err", err)
		return fmt.Errorf("rotate: %w", err)
	}

	player := model.Player{ID: charID, Name: c.Name, Level: c.Level, HP: c.HP, MaxHP: c.MaxHP}
	// The newest connection wins.
	for _, older := range join.Finish(player, welcome) {
		older.End(model.EndLoggedInElsewhere)
	}
	g.sessions.Online(ctx, s)
	g.deps.Observer.OnlineChanged(1)
	g.deps.Logger.InfoContext(ctx, "ws joined", "conn", s.ConnID(), "character_id", charID, "map_id", join.Joined.MapID)
	return nil
}

// rotate has its own budget: a join that retried may have spent the Hello's.
func (g *Gateway) rotate(ctx context.Context, sessionID uuid.UUID) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rotateTimeout)
	defer cancel()
	return g.deps.Sessions.Rotate(ctx, sessionID)
}

// Closed ends a session once its connection closed and none of its commands still runs.
func (g *Gateway) Closed(s *session.Session, reason string) {
	if g.sessions.Close(s) {
		g.deps.Observer.OnlineChanged(-1)
	}
	g.deps.Logger.Info("ws closed", "conn", s.ConnID(), "reason", reason)
}
