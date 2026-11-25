package session

import (
	"context"
	"fmt"
	"time"

	model "finga/internal/model/gateway"
	"finga/internal/shared/kernel"
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
	for _, section := range []welcomeSection{joinedSection, r.inventorySection, r.missedSection} {
		if err := section(ctx, character, joined, &w); err != nil {
			return model.Welcome{}, err
		}
	}
	return w, nil
}

// joinedSection is what the world's join found: the character, its map and tick rate, its quests, its skills' timers and the waypoints found.
func joinedSection(_ context.Context, _ kernel.CharacterID, joined model.Joined, w *model.Welcome) error {
	w.Character, w.MapID, w.TickRateHz, w.Quests, w.Timers, w.ServerTime = joined.Character, joined.MapID, joined.TickRateHz, joined.Quests, joined.Timers, time.Now()
	w.Waypoints = joined.Waypoints
	return nil
}

const toldTimeout = 5 * time.Second // marking a Welcome's missed matches told

// missedSection is the matches that ended while the player was away, marked told once the Welcome is written, so a Welcome lost on the way leaves them for the next; a reconnect just as one ends, or a second login at once, may hear one twice. A failed read logs and fails no login.
func (r *Registry) missedSection(ctx context.Context, character kernel.CharacterID, _ model.Joined, w *model.Welcome) error {
	m, err := r.deps.Pvp.Missed(ctx, character)
	if err != nil {
		r.deps.Logger.WarnContext(ctx, "the matches missed while away wait for the next Welcome", "character", character, "err", err)
		return nil
	}
	w.Missed = m
	told := make([]kernel.MatchID, 0, len(m.Matches)+len(m.Older))
	for _, x := range m.Matches {
		told = append(told, x.Match)
	}
	told = append(told, m.Older...)
	if len(told) == 0 {
		return nil
	}
	ctx = context.WithoutCancel(ctx)
	w.Written = func() {
		r.job(ctx, "gateway.mark_told", func(ctx context.Context) {
			ctx, cancel := context.WithTimeout(ctx, toldTimeout)
			defer cancel()
			if err := r.deps.Pvp.Told(ctx, character, told); err != nil {
				r.deps.Logger.WarnContext(ctx, "a Welcome's missed matches stay untold, for the next to tell again", "character", character, "err", err)
			}
		}, "character", character)
	}
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
