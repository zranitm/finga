package item

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"pgregory.net/rapid"

	"finga/internal/application/gamedata"
	"finga/internal/model"
)

// checkInvariants runs every Stage 1 check and fails the test on a mismatch.
func checkInvariants(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	mismatches, err := CheckInvariants(ctx, pool)
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
	var stack Item
	for _, it := range h.inventory(t) {
		if it.TemplateID == "red_potion" && it.Quantity == 100 {
			stack = it
		}
	}
	if _, err := h.svc.Discard(context.Background(), h.actor, DiscardInput{Key: "i4", ItemID: stack.ID, Quantity: 10}); err != nil {
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
	m5, _ := CheckQuantityMatchesEvents(ctx, h.pool)
	m6, _ := CheckTemplateTotals(ctx, h.pool)
	if len(m5) != 1 || len(m6) != 1 {
		t.Fatalf("checks 5 and 6 found %v and %v, want one mismatch each", m5, m6)
	}
}

// TestInventoryProperties runs random sequences of adds, moves and
// discards and checks the invariants after each sequence.
func TestInventoryProperties(t *testing.T) {
	data, err := gamedata.Load("../../../data")
	if err != nil {
		t.Fatal(err)
	}
	pool := cluster.DB(t)
	store := NewPostgres(pool)
	svc := NewService(store, data, Options{})
	templates := []string{"red_potion", "blue_potion", "slime_gel", "wooden_sword", "leather_cap"}
	tabs := []gamedata.Tab{gamedata.Equip, gamedata.Use, gamedata.Etc}
	ctx := context.Background()

	rapid.Check(t, func(rt *rapid.T) {
		actor := Actor{Account: model.NewAccountID(), Character: model.NewCharacterID()}
		steps := rapid.IntRange(1, 25).Draw(rt, "steps")
		for i := range steps {
			key := fmt.Sprintf("k%d", i)
			switch rapid.IntRange(0, 2).Draw(rt, "op") {
			case 0:
				tmpl := rapid.SampledFrom(templates).Draw(rt, "template")
				it, _ := data.Item(tmpl)
				qty := rapid.IntRange(1, it.MaxStack).Draw(rt, "qty")
				_, err := svc.Add(ctx, actor, AddInput{Key: key, TemplateID: tmpl, Quantity: qty, Cause: Cause{Type: CauseDrop, ID: uuid.New()}})
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
				slot := rapid.IntRange(0, SlotsPerTab-1).Draw(rt, "slot")
				_, err := svc.Move(ctx, actor, MoveInput{Key: key, ItemID: src.ID, Tab: tab, Slot: slot})
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
				_, err := svc.Discard(ctx, actor, DiscardInput{Key: key, ItemID: src.ID, Quantity: qty})
				if err != nil && !isRule(err) {
					rt.Fatalf("discard: %v", err)
				}
			}
		}
		checkInvariants(t, pool)
		// The account's live inventory is consistent with what the service reports.
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

// isRule reports whether err is one of the module's rule errors, which a
// random sequence is expected to hit.
func isRule(err error) bool {
	for _, rule := range []error{ErrInventoryFull, ErrBadQuantity, ErrBadSlot, ErrWrongTab, ErrDifferentItem, ErrNotFound} {
		if errorsIs(err, rule) {
			return true
		}
	}
	return false
}

func errorsIs(err, target error) bool { return errors.Is(err, target) }
