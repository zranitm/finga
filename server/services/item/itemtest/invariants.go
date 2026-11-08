// Package itemtest is the item service's database invariant checks, for its property, race, retry and crash tests.
package itemtest

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/gamedata"
	"finga/internal/progression"
)

// Templates are the item templates the checks hold items to.
type Templates interface {
	Item(id string) (gamedata.Item, bool)
}

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

// templateTotals is check 6: per template, held equals drops and starters less discards; a join would multiply sums.
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

// stacksAndSplitsBalance is check 6's other half: per template, the player's stacks and splits net to zero.
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

// rollsFitTheirTemplates is check 11: every equipment item has one roll, each stat within its template's range, and nothing else has one.
func rollsFitTheirTemplates(ctx context.Context, pool *pgxpool.Pool, tmpls Templates) ([]string, error) {
	rows, err := pool.Query(ctx, `
		SELECT i.id, i.template_id, r.item_id IS NOT NULL,
			coalesce(r.attack, 0), coalesce(r.defense, 0), coalesce(r.str, 0), coalesce(r.dex, 0), coalesce(r."int", 0), coalesce(r.agi, 0), coalesce(r.vit, 0),
			coalesce(r.accuracy, 0), coalesce(r.evasion, 0), coalesce(r.crit_rate, 0), coalesce(r.crit_damage, 0), coalesce(r.max_hp, 0), coalesce(r.max_mp, 0)
		FROM items i LEFT JOIN item_rolls r ON r.item_id = i.id
		ORDER BY i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id uuid.UUID
		var template string
		var rolled bool
		var s progression.ItemStats
		if err := rows.Scan(&id, &template, &rolled, &s.Attack, &s.Defense, &s.STR, &s.DEX, &s.INT, &s.AGI, &s.VIT,
			&s.Accuracy, &s.Evasion, &s.CritRate, &s.CritDamage, &s.MaxHP, &s.MaxMP); err != nil {
			return nil, err
		}
		tmpl, known := tmpls.Item(template)
		equipment := known && tmpl.Tab == gamedata.Equip
		switch {
		case !known:
			out = append(out, fmt.Sprintf("item %s: template %s is not in the game data", id, template))
		case equipment && !rolled:
			out = append(out, fmt.Sprintf("item %s: %s has no roll", id, template))
		case equipment && !s.Fits(tmpl.Rolls):
			out = append(out, fmt.Sprintf("item %s: %s rolled %+v, outside its ranges", id, template, s))
		case !equipment && rolled:
			out = append(out, fmt.Sprintf("item %s: %s is not equipment but has a roll", id, template))
		}
	}
	return out, rows.Err()
}

// dbOnly is a check that needs no game data.
func dbOnly(run func(context.Context, *pgxpool.Pool) ([]string, error)) func(context.Context, *pgxpool.Pool, Templates) ([]string, error) {
	return func(ctx context.Context, pool *pgxpool.Pool, _ Templates) ([]string, error) { return run(ctx, pool) }
}

// checks are the item invariants, in check order.
var checks = []struct {
	name string
	run  func(context.Context, *pgxpool.Pool, Templates) ([]string, error)
}{
	{"5 quantity matches events", dbOnly(quantityMatchesEvents)},
	{"6 template totals", dbOnly(templateTotals)},
	{"6 stacks and splits balance", dbOnly(stacksAndSplitsBalance)},
	{"8 one item per slot", dbOnly(oneItemPerSlot)},
	{"11 rolls fit their templates", rollsFitTheirTemplates},
}

// CheckInvariants runs every check against the game data's templates and returns all mismatches, each named by its check, in check order.
func CheckInvariants(ctx context.Context, pool *pgxpool.Pool, tmpls Templates) ([]string, error) {
	var all []string
	for _, c := range checks {
		mismatches, err := c.run(ctx, pool, tmpls)
		if err != nil {
			return nil, fmt.Errorf("check %s: %w", c.name, err)
		}
		for _, m := range mismatches {
			all = append(all, c.name+": "+m)
		}
	}
	return all, nil
}
