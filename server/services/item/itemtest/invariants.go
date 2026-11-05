// Package itemtest is test support for the item service: its invariant checks over its database,
// for the property, race and retry tests and the crash test in test/. Only tests import it.
package itemtest

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// quantityMatchesEvents is check 5: each item's quantity equals its events' delta sum.
func quantityMatchesEvents(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
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

// templateTotals is check 6: per template, the quantity held equals what drops and starter grants
// created or stacked, less what was discarded. Moves, stacks and splits by the player only shift it.
// Items and events are summed apart; a row join would multiply quantities by event count.
func templateTotals(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT held.template_id, held.quantity, coalesce(ev.delta, 0)
		FROM (SELECT template_id, sum(quantity) AS quantity FROM items GROUP BY template_id) held
		LEFT JOIN (
			SELECT i.template_id, sum(e.quantity_delta) AS delta
			FROM item_events e JOIN items i ON i.id = e.item_id
			WHERE (e.kind IN ('created', 'stacked') AND e.cause_type IN ('drop', 'starter')) OR e.kind = 'discarded'
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
		out = append(out, fmt.Sprintf("template %s: holds %d, drops and starters less discards come to %d", template, quantity, delta))
	}
	return out, rows.Err()
}

// stacksAndSplitsBalance is check 6's other half: per template, what the player stacked equals what
// they split off, as a merge takes from one stack what it puts on another.
func stacksAndSplitsBalance(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT i.template_id, sum(e.quantity_delta)
		FROM item_events e JOIN items i ON i.id = e.item_id
		WHERE e.kind IN ('stacked', 'split') AND e.cause_type = 'player'
		GROUP BY i.template_id
		HAVING sum(e.quantity_delta) <> 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var template string
		var net int64
		if err := rows.Scan(&template, &net); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("template %s: the player's stacks and splits net %d", template, net))
	}
	return out, rows.Err()
}

// oneItemPerSlot is check 8: no two inventory items share a slot.
func oneItemPerSlot(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
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

// checks are the Stage 1 checks of page 6, in its order.
var checks = []struct {
	name string
	run  func(context.Context, *pgxpool.Pool) ([]string, error)
}{
	{"5 quantity matches events", quantityMatchesEvents},
	{"6 template totals", templateTotals},
	{"6 stacks and splits balance", stacksAndSplitsBalance},
	{"8 one item per slot", oneItemPerSlot},
}

// CheckInvariants runs every Stage 1 check and returns all mismatches, each named by its check, in check order.
func CheckInvariants(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	var all []string
	for _, c := range checks {
		mismatches, err := c.run(ctx, pool)
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", c.name, err)
		}
		for _, m := range mismatches {
			all = append(all, c.name+": "+m)
		}
	}
	return all, nil
}
