// Package invariants holds the item service's Stage 1 invariant checks
// (Low-level design, page 6: 5, 6 and 8) as plain functions over its
// tables. It sits outside the service's internal/ on purpose: the item
// module's tests, the end-to-end tests and the crash test run them, and the
// Stage 2 auditor will.
package invariants

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Each check returns the mismatches it found; any mismatch is a failure.

// CheckQuantityMatchesEvents is check 5: each item's quantity equals the
// sum of its item events' quantity deltas.
func CheckQuantityMatchesEvents(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
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

// CheckTemplateTotals is check 6: per template, the total quantity held
// equals what was created minus what was discarded. Every change is an
// event, so the events of a template's items sum to its live quantity.
// Items and events are summed apart, then compared: joining them row by
// row would multiply each quantity by its number of events.
func CheckTemplateTotals(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
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

// CheckOneItemPerSlot is check 8: no two inventory items share a slot.
func CheckOneItemPerSlot(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
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

// CheckInvariants runs every Stage 1 check and returns all mismatches.
func CheckInvariants(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	var all []string
	for name, check := range map[string]func(context.Context, *pgxpool.Pool) ([]string, error){
		"5 quantity matches events": CheckQuantityMatchesEvents,
		"6 template totals":         CheckTemplateTotals,
		"8 one item per slot":       CheckOneItemPerSlot,
	} {
		mismatches, err := check(ctx, pool)
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", name, err)
		}
		for _, m := range mismatches {
			all = append(all, name+": "+m)
		}
	}
	return all, nil
}
