package session

import (
	"context"
	"fmt"
	"time"

	"finga/internal/kernel"
	"finga/modules/gateway/internal/model"
)

// Items reads a character's inventory for its Welcome.
type Items interface {
	Inventory(ctx context.Context, character kernel.CharacterID) (model.Inventory, error)
}

// welcomeSection fills its part of a Welcome; a later part of the screen, such as skills, is one more.
type welcomeSection func(ctx context.Context, character kernel.CharacterID, joined model.Joined, w *model.Welcome) error

// Welcome is what the player's screen needs at once, after a login or a rejoin, built section by section. What a window opens on demand loads with that window, never here. The token is the login's to add.
func (r *Registry) Welcome(ctx context.Context, character kernel.CharacterID, joined model.Joined) (model.Welcome, error) {
	var w model.Welcome
	for _, section := range []welcomeSection{joinedSection, r.inventorySection} {
		if err := section(ctx, character, joined, &w); err != nil {
			return model.Welcome{}, err
		}
	}
	return w, nil
}

// joinedSection is what the world's join found: the character, its map and tick rate, its quests, and its skills' timers.
func joinedSection(_ context.Context, _ kernel.CharacterID, joined model.Joined, w *model.Welcome) error {
	w.Character, w.MapID, w.TickRateHz, w.Quests, w.Timers, w.ServerTime = joined.Character, joined.MapID, joined.TickRateHz, joined.Quests, joined.Timers, time.Now()
	return nil
}

// inventorySection is the inventory and what is worn, from one read; the party follows in a PartyState, once Online registers the player.
func (r *Registry) inventorySection(ctx context.Context, character kernel.CharacterID, _ model.Joined, w *model.Welcome) error {
	inv, err := r.deps.Items.Inventory(ctx, character)
	if err != nil {
		return fmt.Errorf("inventory: %w", err)
	}
	w.Inventory, w.Equipment, w.InventoryRevision = inv.Items, inv.Equipment, inv.Revision
	return nil
}
