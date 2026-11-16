// Package local adapts the item module, in the same process, to the character module's ports.
package local

import (
	"context"

	"finga/internal/kernel"
	"finga/modules/item"
)

// Items is the item module as character creation calls it.
type Items interface {
	GrantStarter(ctx context.Context, actor item.Actor) (item.Result, error)
}

// Starter is character creation's StarterItems over the item module.
type Starter struct{ items Items }

// NewStarter adapts the item module.
func NewStarter(items Items) Starter { return Starter{items: items} }

// GrantStarter asks for the starter items, keyed by the character.
func (s Starter) GrantStarter(ctx context.Context, account kernel.AccountID, character kernel.CharacterID) error {
	_, err := s.items.GrantStarter(ctx, item.Actor{Account: account, Character: character})
	return err
}

// Retryable is a failure that breaks no final rule, a module's own failure among them.
func (Starter) Retryable(err error) bool { return !kernel.IsRule(err) }
