// Package clients are the character service's adapters for the services it
// calls, in the character module's own terms: the item service, for a new
// character's starter items.
package clients

import (
	"context"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/model"
	"finga/pkg/grpcx"
)

// Starter is character creation's StarterItems over the item service.
type Starter struct{ c itemv1.ItemServiceClient }

// NewStarter adapts the item client.
func NewStarter(c itemv1.ItemServiceClient) Starter { return Starter{c: c} }

// GrantStarter asks for the class's starter items, keyed by the character.
func (s Starter) GrantStarter(ctx context.Context, account model.AccountID, character model.CharacterID, class model.Class) error {
	_, err := s.c.GrantStarter(ctx, &itemv1.GrantStarterRequest{
		Actor: &itemv1.Actor{AccountId: account.String(), CharacterId: character.String()},
		Class: class.String(),
	})
	return err
}

// Retryable is a failure that carries no reason: the item service was not
// reached, or could not explain it. A reason is a rule; retrying cannot help.
func (Starter) Retryable(err error) bool { return !grpcx.IsRule(err) }
