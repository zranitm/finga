package account

import (
	"context"
	"fmt"
	"time"

	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
)

// ConnectionsItems reads a character's inventory for its Welcome.
type ConnectionsItems interface {
	Inventory(ctx context.Context, character kernel.CharacterID) (any, error)
}

// welcomeSection fills its part of a Welcome; a later part of the screen, such as skills, is one more.
type welcomeSection func(ctx context.Context, character kernel.CharacterID, joined model.Joined, w *model.Welcome) error

// Welcome is what the player's screen needs at once, after a login or a rejoin, built section by section. What a window opens on demand loads with that window, never here. The token is the login's to add.
func (r *Connections) Welcome(ctx context.Context, character kernel.CharacterID, joined model.Joined) (model.Welcome, error) {
	var w model.Welcome
	for _, section := range []welcomeSection{joinedSection, r.inventorySection, r.missedSection} {
		if err := section(ctx, character, joined, &w); err != nil {
			return model.Welcome{}, err
		}
	}
	return w, nil
}

// joinedSection is what the world's join found: the player, its map and tick rate, the waypoints found, and the world's own view of the character, which its handler reads.
func joinedSection(_ context.Context, _ kernel.CharacterID, joined model.Joined, w *model.Welcome) error {
	w.Player, w.MapID, w.TickRateHz, w.ServerTime = joined.Player, joined.MapID, joined.TickRateHz, time.Now()
	w.Waypoints, w.Joined = joined.Waypoints, joined.View
	return nil
}

const (
	missedTimeout = 2 * time.Second // reading a Welcome's missed matches, so a slow pvp store never holds up a login
	toldTimeout   = 5 * time.Second // marking them told
)

// missedSection is the matches that ended while the player was away, marked told once the Welcome is written, so a Welcome lost on the way leaves them for the next; a reconnect just as one ends, or a second login at once, may hear one twice. A failed or slow read logs and fails no login.
func (r *Connections) missedSection(ctx context.Context, character kernel.CharacterID, _ model.Joined, w *model.Welcome) error {
	readCtx, cancel := context.WithTimeout(ctx, missedTimeout)
	m, err := r.deps.Pvp.Missed(readCtx, character)
	cancel()
	if err != nil {
		r.deps.Logger.WarnContext(ctx, "the matches missed while away wait for the next Welcome", "character", character, "err", err)
		return nil
	}
	w.Missed = m.Details
	told := m.Told
	if len(told) == 0 {
		return nil
	}
	ctx = context.WithoutCancel(ctx)
	w.Written = func() {
		r.job(ctx, "account.mark_told", func(ctx context.Context) {
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
func (r *Connections) inventorySection(ctx context.Context, character kernel.CharacterID, _ model.Joined, w *model.Welcome) error {
	inv, err := r.deps.Items.Inventory(ctx, character)
	if err != nil {
		return fmt.Errorf("inventory: %w", err)
	}
	w.Inventory = inv
	return nil
}
