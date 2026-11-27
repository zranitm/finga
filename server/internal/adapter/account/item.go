package account

import (
	"context"

	"finga/internal/application/item"
	"finga/internal/shared/kernel"
)

// ItemModule is the item module as the gateway calls it.
type ItemModule interface {
	Inventory(ctx context.Context, owner kernel.CharacterID) (item.Inventory, error)
}

// Items is the application's Items over the item module.
type Items struct{ items ItemModule }

// NewItems adapts the item module.
func NewItems(items ItemModule) Items { return Items{items: items} }

// Inventory is the character's inventory as the item module has it; the item handler reads it for the Welcome.
func (i Items) Inventory(ctx context.Context, character kernel.CharacterID) (any, error) {
	got, err := i.items.Inventory(ctx, character)
	if err != nil {
		return nil, wrap(err)
	}
	return got, nil
}
