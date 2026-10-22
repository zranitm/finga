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

// The Stage 1 invariant checks (Low-level design, page 6: 5, 6 and 8).
// Each returns the mismatches it found; the auditor runs the same checks
// continuously in Stage 2, from internal/application/audit.

// checkQuantityMatchesEvents is check 5: each item's quantity equals the
// sum of its item events' quantity deltas.
func checkQuantityMatchesEvents(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT i.id, i.quantity, coalesce(sum(e.quantity_delta), 0)
		FROM items i LEFT JOIN item_events e ON e.item_id = i.id
		GROUP BY i.id, i.quantity
		HAVING i.quantity <> coalesce(sum(e.quantity_delta), 0)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id uuid.UUID
		var quantity, delta int64
		if err := rows.Scan(&id, &quantity, &delta); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("item %s: quantity %d, events sum to %d", id, quantity, delta))
	}
	return out, rows.Err()
}

// checkTemplateTotals is check 6: per template, the total quantity held
// equals what was created minus what was discarded (every change is an
// event, so the events of a template sum to its live quantity).
func checkTemplateTotals(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	// Items and events are summed apart, then compared: joining them row by
	// row would multiply each quantity by its number of events.
	rows, err := pool.Query(ctx, `
		SELECT held.template_id, held.quantity, coalesce(ev.delta, 0)
		FROM (SELECT template_id, sum(quantity) AS quantity FROM items GROUP BY template_id) held
		LEFT JOIN (
			SELECT i.template_id, sum(e.quantity_delta) AS delta
			FROM item_events e JOIN items i ON i.id = e.item_id
			GROUP BY i.template_id
		) ev USING (template_id)
		WHERE held.quantity <> coalesce(ev.delta, 0)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var template string
		var quantity, delta int64
		if err := rows.Scan(&template, &quantity, &delta); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("template %s: holds %d, events sum to %d", template, quantity, delta))
	}
	return out, rows.Err()
}

// checkOneItemPerSlot is check 8: no two inventory items share a slot.
func checkOneItemPerSlot(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT owner_id, tab, slot, count(*) FROM items
		WHERE location = 'inventory'
		GROUP BY owner_id, tab, slot HAVING count(*) > 1`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var owner uuid.UUID
		var tab string
		var slot, n int
		if err := rows.Scan(&owner, &tab, &slot, &n); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("owner %s %s slot %d: %d items", owner, tab, slot, n))
	}
	return out, rows.Err()
}

// checkInvariants runs every Stage 1 check and fails the test on a mismatch.
func checkInvariants(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for name, check := range map[string]func(context.Context, *pgxpool.Pool) ([]string, error){
		"5 quantity matches events": checkQuantityMatchesEvents,
		"6 template totals":         checkTemplateTotals,
		"8 one item per slot":       checkOneItemPerSlot,
	} {
		mismatches, err := check(ctx, pool)
		if err != nil {
			t.Fatalf("check %s: %v", name, err)
		}
		if len(mismatches) > 0 {
			t.Fatalf("check %s failed: %v", name, mismatches)
		}
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
	m5, _ := checkQuantityMatchesEvents(ctx, h.pool)
	m6, _ := checkTemplateTotals(ctx, h.pool)
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
