package app

import (
	"context"

	"github.com/google/uuid"

	accountv1 "finga/api/gen/account/v1"
	itemv1 "finga/api/gen/item/v1"
	"finga/internal/application/world"
	"finga/internal/gamedata"
	"finga/internal/model"
	"finga/pkg/auth"
	"finga/pkg/grpcx"
)

// The adapters below let the modules still inside the game server call the
// services already extracted (page 10, decision 185), in the modules' own
// terms. Each moves into its caller's service when that one leaves too.

// pickups is the world's Pickups over the item service.
type pickups struct{ c itemv1.ItemServiceClient }

// Add puts a drop in the inventory, keyed and caused by the drop's ID, so a
// retry or a replay grants it once.
func (p pickups) Add(ctx context.Context, in world.Pickup) ([]world.Slot, error) {
	drop := in.DropID.String()
	res, err := p.c.Add(ctx, &itemv1.AddRequest{
		Actor:          &itemv1.Actor{AccountId: in.Account.String(), CharacterId: in.Character.String()},
		IdempotencyKey: drop,
		TemplateId:     in.Template,
		Quantity:       int32(in.Quantity),
		Cause:          &itemv1.Cause{Type: itemv1.CauseType_CAUSE_TYPE_DROP, Id: drop},
	})
	if err != nil {
		return nil, err
	}
	out := make([]world.Slot, 0, len(res.GetChanged()))
	for _, s := range res.GetChanged() {
		slot := world.Slot{Tab: gamedata.Tab(s.GetTab()), Slot: int(s.GetSlot())}
		if it := s.GetItem(); it != nil {
			id, err := model.ParseItemID(it.GetId())
			if err != nil {
				return nil, err
			}
			slot.Item = &world.SlotItem{ID: id, Template: it.GetTemplateId(), Quantity: int(it.GetQuantity())}
		}
		out = append(out, slot)
	}
	return out, nil
}

// Retryable is a failure that carries no reason: the item service was not
// reached, or could not explain it. A reason is a rule; retrying cannot help.
func (pickups) Retryable(err error) bool { return !grpcx.IsRule(err) }

// starter is character creation's StarterItems over the item service.
type starter struct{ c itemv1.ItemServiceClient }

// GrantStarter asks for the class's starter items, keyed by the character.
func (s starter) GrantStarter(ctx context.Context, account model.AccountID, character model.CharacterID, class model.Class) error {
	_, err := s.c.GrantStarter(ctx, &itemv1.GrantStarterRequest{
		Actor: &itemv1.Actor{AccountId: account.String(), CharacterId: character.String()},
		Class: class.String(),
	})
	return err
}

// Retryable is a failure that carries no reason.
func (starter) Retryable(err error) bool { return !grpcx.IsRule(err) }

// guard is the REST handler's and the gateway's auth.Guard over the account
// service. Failures pass through as they came, with their reason.
type guard struct {
	c accountv1.AccountServiceClient
}

// Validate resolves a token to its principal.
func (g guard) Validate(ctx context.Context, token string) (auth.Principal, error) {
	res, err := g.c.Validate(ctx, &accountv1.ValidateRequest{Token: token})
	if err != nil {
		return auth.Principal{}, err
	}
	account, err := uuid.Parse(res.GetAccountId())
	if err != nil {
		return auth.Principal{}, err
	}
	session, err := uuid.Parse(res.GetSessionId())
	if err != nil {
		return auth.Principal{}, err
	}
	return auth.Principal{AccountID: account, SessionID: session}, nil
}

// Revoke ends one session.
func (g guard) Revoke(ctx context.Context, sessionID uuid.UUID) error {
	_, err := g.c.Revoke(ctx, &accountv1.RevokeRequest{SessionId: sessionID.String()})
	return err
}

// Rotate issues a session's next token.
func (g guard) Rotate(ctx context.Context, sessionID uuid.UUID) (string, error) {
	res, err := g.c.Rotate(ctx, &accountv1.RotateRequest{SessionId: sessionID.String()})
	return res.GetToken(), err
}

// Check reports whether a session is still active.
func (g guard) Check(ctx context.Context, sessionID uuid.UUID) error {
	_, err := g.c.Check(ctx, &accountv1.CheckRequest{SessionId: sessionID.String()})
	return err
}

// RevokeAll ends every session of an account.
func (g guard) RevokeAll(ctx context.Context, accountID uuid.UUID) error {
	_, err := g.c.RevokeAll(ctx, &accountv1.RevokeAllRequest{AccountId: accountID.String()})
	return err
}
