package send

import (
	"context"
	"fmt"

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
		return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
	}
	// The client keeps Welcome's token; the one it sent works once more for a minute.
	rotated, err := g.deps.Sessions.Rotate(ctx, p.SessionID)
	if err != nil {
		if model.PublicOf(err) == model.Unauthenticated {
			return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
		}
		g.deps.Logger.ErrorContext(ctx, "token rotation failed", "session_id", p.SessionID, "err", err)
		return fmt.Errorf("rotate: %w", err)
	}

	join, err := g.sessions.Join(ctx, s, kernel.AccountID(p.AccountID), kernel.SessionID(p.SessionID), charID)
	if err != nil {
		if model.PublicOf(err).Code == model.CharacterNotFound {
			return fmt.Errorf("%w: %w", model.ErrHelloRejected, err)
		}
		g.deps.Logger.ErrorContext(ctx, "join failed", "character_id", charID, "err", err)
		return fmt.Errorf("join: %w", err)
	}
	c := join.Joined.Character
	welcome := model.Welcome{Token: rotated, Character: c, MapID: join.Joined.MapID, ServerTime: g.deps.Clock()}
	if g.deps.Items != nil {
		items, err := g.deps.Items.Inventory(ctx, charID)
		if err != nil {
			join.Cancel()
			g.deps.Logger.ErrorContext(ctx, "inventory failed", "character_id", charID, "err", err)
			return fmt.Errorf("inventory: %w", err)
		}
		welcome.Inventory = items
	}
	if g.deps.Social != nil {
		// Parties are optional: a social failure must not block the join.
		party, err := g.deps.Social.PartyOf(ctx, charID)
		if err != nil {
			g.deps.Logger.WarnContext(ctx, "party lookup failed", "character_id", charID, "err", err)
		}
		welcome.Party = party
	}

	player := model.Player{ID: charID, Name: c.GetName(), Level: int(c.GetLevel()), HP: int(c.GetHp()), MaxHP: int(c.GetMaxHp())}
	// The newest connection wins.
	for _, older := range join.Finish(player, welcome) {
		older.End(model.EndLoggedInElsewhere)
	}
	g.sessions.Online(ctx, s)
	g.deps.Observer.OnlineChanged(1)
	g.deps.Logger.InfoContext(ctx, "ws joined", "conn", s.ConnID(), "character_id", charID, "map_id", join.Joined.MapID)
	return nil
}

// Closed ends a session once its connection closed and none of its commands still runs.
func (g *Gateway) Closed(s *session.Session, reason string) {
	if g.sessions.Remove(s) {
		g.sessions.Leave(s)
	}
	if s.Active() {
		if g.deps.Social != nil {
			ctx, cancel := context.WithTimeout(context.Background(), offlineTimeout)
			if err := g.deps.Social.Offline(ctx, s.Character()); err != nil {
				g.deps.Logger.WarnContext(ctx, "social offline failed", "character_id", s.Character(), "err", err)
			}
			cancel()
		}
		g.deps.Observer.OnlineChanged(-1)
	}
	g.deps.Logger.Info("ws closed", "conn", s.ConnID(), "reason", reason)
}
