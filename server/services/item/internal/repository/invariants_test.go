package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"pgregory.net/rapid"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/uuidv7"
	"finga/services/item/internal/application"
	"finga/services/item/internal/model"
	"finga/services/item/itemtest"
)

func checkInvariants(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	mismatches, err := itemtest.CheckInvariants(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(mismatches) > 0 {
		t.Fatalf("invariants failed: %v", mismatches)
	}
}

func TestInvariantsHoldAfterTheServiceTests(t *testing.T) {
	h := newHarness(t)
	h.add(t, "i1", "red_potion", 70)
	h.add(t, "i2", "red_potion", 70)
	h.add(t, "i3", "wooden_sword", 1)
	var stack model.Item
	for _, it := range h.inventory(t) {
		if it.TemplateID == "red_potion" && it.Quantity == 100 {
			stack = it
		}
	}
	if _, err := h.svc.Discard(context.Background(), h.actor, application.DiscardInput{Key: "i4", ItemID: stack.ID, Quantity: 10}); err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, h.pool)
}

func TestInvariantChecksDetectCorruption(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	h.add(t, "c1", "red_potion", 10)
	inv := h.inventory(t)
	// Corrupt the quantity behind the module's back.
	if _, err := h.pool.Exec(ctx, `UPDATE items SET quantity = 11 WHERE id = $1`, inv[0].ID); err != nil {
		t.Fatal(err)
	}
	for range 5 { // the same order every run
		got, err := itemtest.CheckInvariants(ctx, h.pool)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || !strings.HasPrefix(got[0], "5 ") || !strings.HasPrefix(got[1], "6 ") {
			t.Fatalf("found %v, want check 5's mismatch, then check 6's", got)
		}
	}
}

// Corruptions whose events still sum to each item's quantity pass check 5; check 6 sees them.
func TestCheckSixCatchesWhatCheckFiveCannot(t *testing.T) {
	ctx := context.Background()
	t.Run("a drop recorded as the player's own", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "c1", "red_potion", 10)
		if _, err := h.pool.Exec(ctx, `UPDATE item_events SET cause_type = 'player' WHERE kind = 'created'`); err != nil {
			t.Fatal(err)
		}
		got, err := itemtest.CheckInvariants(ctx, h.pool)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || !strings.HasPrefix(got[0], "6 template totals") {
			t.Fatalf("found %v, want check 6's template totals alone", got)
		}
	})
	t.Run("a merge whose split half is no longer the player's", func(t *testing.T) {
		h := newHarness(t)
		h.add(t, "c2", "red_potion", 100) // a full stack in slot 0
		h.add(t, "c3", "red_potion", 30)  // 30 more open slot 1
		if _, err := h.svc.Discard(ctx, h.actor, application.DiscardInput{Key: "c4", ItemID: h.inventory(t)[0].ID, Quantity: 20}); err != nil {
			t.Fatal(err)
		}
		var small model.Item
		for _, it := range h.inventory(t) {
			if it.Quantity == 30 {
				small = it
			}
		}
		if _, err := h.svc.Move(ctx, h.actor, application.MoveInput{Key: "c5", ItemID: small.ID, Tab: gamedata.Use, Slot: 0}); err != nil {
			t.Fatal(err)
		}
		if _, err := h.pool.Exec(ctx, `UPDATE item_events SET cause_type = 'drop' WHERE kind = 'split'`); err != nil {
			t.Fatal(err)
		}
		got, err := itemtest.CheckInvariants(ctx, h.pool)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || !strings.HasPrefix(got[0], "6 stacks and splits") {
			t.Fatalf("found %v, want check 6's stacks and splits alone", got)
		}
	})
}

func TestInventoryProperties(t *testing.T) {
	data, err := gamedata.Load("../../../../data")
	if err != nil {
		t.Fatal(err)
	}
	pool := cluster.DB(t)
	store := NewPostgres(pool)
	svc := application.NewService(store, data)
	templates := []string{"red_potion", "blue_potion", "slime_gel", "wooden_sword", "leather_cap"}
	tabs := []gamedata.Tab{gamedata.Equip, gamedata.Use, gamedata.Etc}
	ctx := context.Background()

	rapid.Check(t, func(rt *rapid.T) {
		actor := model.Actor{Account: kernel.NewAccountID(), Character: kernel.NewCharacterID()}
		steps := rapid.IntRange(1, 25).Draw(rt, "steps")
		for i := range steps {
			key := fmt.Sprintf("k%d", i)
			switch rapid.IntRange(0, 2).Draw(rt, "op") {
			case 0:
				tmpl := rapid.SampledFrom(templates).Draw(rt, "template")
				it, _ := data.Item(tmpl)
				qty := rapid.IntRange(1, it.MaxStack).Draw(rt, "qty")
				_, err := svc.Add(ctx, actor, application.AddInput{Key: key, TemplateID: tmpl, Quantity: qty, Cause: model.Cause{Type: model.CauseDrop, ID: uuidv7.New()}})
				if err != nil && !isRule(err) {
					rt.Fatalf("add: %v", err)
				}
			case 1:
				inv, _ := svc.Inventory(ctx, actor.Character)
				if len(inv) == 0 {
					continue
				}
				src := rapid.SampledFrom(inv).Draw(rt, "item")
				tab := src.Tab
				if rapid.Bool().Draw(rt, "wrong tab") {
					tab = rapid.SampledFrom(tabs).Draw(rt, "tab")
				}
				slot := rapid.IntRange(0, model.SlotsPerTab-1).Draw(rt, "slot")
				_, err := svc.Move(ctx, actor, application.MoveInput{Key: key, ItemID: src.ID, Tab: tab, Slot: slot})
				if err != nil && !isRule(err) {
					rt.Fatalf("move: %v", err)
				}
			case 2:
				inv, _ := svc.Inventory(ctx, actor.Character)
				if len(inv) == 0 {
					continue
				}
				src := rapid.SampledFrom(inv).Draw(rt, "item")
				qty := rapid.IntRange(1, src.Quantity+1).Draw(rt, "qty")
				_, err := svc.Discard(ctx, actor, application.DiscardInput{Key: key, ItemID: src.ID, Quantity: qty})
				if err != nil && !isRule(err) {
					rt.Fatalf("discard: %v", err)
				}
			}
		}
		checkInvariants(t, pool)
		inv, err := svc.Inventory(ctx, actor.Character)
		if err != nil {
			rt.Fatal(err)
		}
		seen := map[string]bool{}
		for _, it := range inv {
			k := fmt.Sprintf("%s/%d", it.Tab, it.Slot)
			if seen[k] || it.Quantity < 1 {
				rt.Fatalf("inventory %+v", inv)
			}
			seen[k] = true
		}
	})
}

// isRule reports whether err is a rule error, which random sequences are expected to hit.
func isRule(err error) bool {
	for _, rule := range []error{model.ErrInventoryFull, model.ErrBadQuantity, model.ErrBadSlot, model.ErrWrongTab, model.ErrDifferentItem, model.ErrStackFull, model.ErrNotFound} {
		if errorsIs(err, rule) {
			return true
		}
	}
	return false
}

func errorsIs(err, target error) bool { return errors.Is(err, target) }
