package migrations_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/gamedata"
	"finga/pkg/idempotency/idempotencypg/idempotencypgtest"
	"finga/pkg/outbox/outboxpg/outboxpgtest"
	"finga/pkg/pgtest"
	"finga/pkg/uuidv7"
	"finga/services/item/itemtest"
	"finga/services/item/migrations"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, nil)
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

// The outbox and idempotency tables here must match the Schema their test support holds.
func TestTheLibraryTablesMatchTheirSchemas(t *testing.T) {
	ctx := context.Background()
	migrated := cluster.DB(t)
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, migrated); err != nil {
		t.Fatal(err)
	}
	reference := cluster.DB(t)
	for _, ddl := range []string{outboxpgtest.Schema, idempotencypgtest.Schema} {
		if _, err := reference.Exec(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	for _, table := range []string{"outbox", "idempotency_keys"} {
		t.Run(table, func(t *testing.T) {
			if got, want := shape(t, migrated, table), shape(t, reference, table); !slices.Equal(got, want) {
				t.Fatalf("migration:\n%v\nlibrary schema:\n%v", got, want)
			}
		})
	}
}

// shape lists a table's columns, in order and with their defaults, and its index definitions.
func shape(t *testing.T, pool *pgxpool.Pool, table string) []string {
	t.Helper()
	ctx := context.Background()
	var out []string
	rows, err := pool.Query(ctx, `
		SELECT column_name || ' ' || data_type || ' ' || is_nullable || coalesce(' ' || column_default, '')
		FROM information_schema.columns WHERE table_name = $1 ORDER BY ordinal_position`, table)
	if err != nil {
		t.Fatal(err)
	}
	cols, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, cols...)
	rows, err = pool.Query(ctx, `SELECT indexdef FROM pg_indexes WHERE tablename = $1 ORDER BY indexname`, table)
	if err != nil {
		t.Fatal(err)
	}
	idx, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	return append(out, idx...)
}

// Grants made before starter_grants existed, known only by their idempotency keys, are carried over.
func TestStarterGrantsAreBackfilledFromTheirKeys(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	before := fstest.MapFS{}
	for _, name := range []string{"00001_platform.sql", "00002_item.sql"} {
		b, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			t.Fatal(err)
		}
		before[name] = &fstest.MapFile{Data: b}
	}
	if err := pgtest.Migrations(before, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	character := uuidv7.New()
	if _, err := pool.Exec(ctx, `INSERT INTO idempotency_keys (owner, scope, key, request_hash, response, created_at)
		VALUES ($1, 'item.starter', $2, '\x00', '\x00', now()), ($1, 'item.add', $3, '\x00', '\x00', now())`,
		uuidv7.NewString(), character.String(), uuidv7.NewString()); err != nil {
		t.Fatal(err)
	}
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var ids []uuid.UUID
	rows, err := pool.Query(ctx, `SELECT character_id FROM starter_grants`)
	if err != nil {
		t.Fatal(err)
	}
	ids, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil || len(ids) != 1 || ids[0] != character {
		t.Fatalf("starter grants %v (%v), want the one character", ids, err)
	}
}

const appStamps = "00006_app_stamps.sql"

const timestamps = "00004_timestamps.sql"

func TestEveryTableHasItsTimestamps(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := pgtest.CheckTimestamps(ctx, pool, migrations.Table); err != nil {
		t.Fatal(err)
	}
}

// The services share one database and set_updated_at(); the last to stop using it drops it.
func TestTheStampFunctionGoesWithItsLastTrigger(t *testing.T) {
	ctx := context.Background()
	exists := func(t *testing.T, pool *pgxpool.Pool) bool {
		t.Helper()
		var ok bool
		if err := pool.QueryRow(ctx, `SELECT to_regproc('set_updated_at') IS NOT NULL`).Scan(&ok); err != nil {
			t.Fatal(err)
		}
		return ok
	}
	t.Run("alone, the service drops it", func(t *testing.T) {
		pool := cluster.DB(t)
		if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
			t.Fatal(err)
		}
		if exists(t, pool) {
			t.Fatal("set_updated_at() outlived its triggers")
		}
	})
	t.Run("another service's trigger keeps it", func(t *testing.T) {
		pool := cluster.DB(t)
		if err := pgtest.MigrationsBefore(migrations.FS, migrations.Table, appStamps)(ctx, pool); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `CREATE TABLE other (updated_at timestamptz);
			CREATE TRIGGER set_updated_at BEFORE UPDATE ON other FOR EACH ROW EXECUTE FUNCTION set_updated_at();`); err != nil {
			t.Fatal(err)
		}
		if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
			t.Fatal(err)
		}
		if !exists(t, pool) {
			t.Fatal("set_updated_at() was dropped while another table uses it")
		}
	})
}

// Rows from before the timestamps migration get the best times they can.
func TestTimestampsAreBackfilled(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.MigrationsBefore(migrations.FS, migrations.Table, timestamps)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return t0.Add(time.Duration(h) * time.Hour) }
	moved, quiet, character := uuidv7.New(), uuidv7.New(), uuidv7.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO items (id, template_id, owner_id, location, tab, slot, quantity, updated_at) VALUES
			($1, 'red_potion', $3, 'inventory', 'use', 0, 5, $4), ($2, 'red_potion', $3, 'inventory', 'use', 1, 5, $5)`,
			[]any{moved, quiet, character, at(2), at(3)}},
		{`INSERT INTO item_events (item_id, kind, quantity_delta, cause_type, occurred_at) VALUES
			($1, 'created', 5, 'drop', $2), ($1, 'moved', 0, 'player', $3)`, []any{moved, at(0), at(2)}},
		{`INSERT INTO starter_grants (character_id, granted_at) VALUES ($1, $2)`, []any{character, at(1)}},
		{`INSERT INTO outbox (event_id, topic, key, type, content_type, payload, occurred_at, published_at) VALUES
			($1, 'item', 'k', 'ItemLooted', 'application/json', '{}', $3, NULL),
			($2, 'item', 'k', 'ItemLooted', 'application/json', '{}', $3, $4)`, []any{uuidv7.New(), uuidv7.New(), at(4), at(5)}},
		{`INSERT INTO idempotency_keys (owner, scope, key, request_hash, response, created_at)
			VALUES ('a', 'item.move', 'k', '\x00', '\x00', $1)`, []any{at(6)}},
	} {
		if _, err := pool.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		query string // created_at and updated_at, in a stable row order
		want  []time.Time
	}{
		{"an item was created at its first event, else at its last change", `SELECT created_at, updated_at FROM items ORDER BY updated_at`, []time.Time{at(0), at(2), at(3), at(3)}},
		{"an item event was written when it occurred", `SELECT created_at, updated_at FROM item_events ORDER BY id`, []time.Time{at(0), at(0), at(2), at(2)}},
		{"a starter grant keeps its time as created_at", `SELECT created_at, updated_at FROM starter_grants`, []time.Time{at(1), at(1)}},
		{"an outbox event was last changed when published", `SELECT created_at, updated_at FROM outbox ORDER BY id`, []time.Time{at(4), at(4), at(4), at(5)}},
		{"an idempotency key was last changed when created", `SELECT created_at, updated_at FROM idempotency_keys`, []time.Time{at(6), at(6)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows, err := pool.Query(ctx, tt.query)
			if err != nil {
				t.Fatal(err)
			}
			var got []time.Time
			for rows.Next() {
				var created, updated time.Time
				if err := rows.Scan(&created, &updated); err != nil {
					t.Fatal(err)
				}
				got = append(got, created, updated)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if !slices.EqualFunc(got, tt.want, time.Time.Equal) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

const rolls = "00008_item_rolls.sql"

// Equipment from before rolls, even destroyed, gets its template's lowest roll as items.yaml had it when rolls came, and anything else none, so check 11 holds from the first start.
func TestEquipmentIsBackfilledWithItsLowestRoll(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.MigrationsBefore(migrations.FS, migrations.Table, rolls)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	data, err := gamedata.Load("../../../data")
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	owner := uuidv7.New()
	add := func(template, location string, slot int) uuid.UUID {
		t.Helper()
		tmpl, _ := data.Item(template)
		id := uuidv7.New()
		var tab *string
		var inSlot *int
		quantity := 0
		if location == "inventory" {
			tb := string(tmpl.Tab)
			tab, inSlot, quantity = &tb, &slot, 1
		}
		if _, err := pool.Exec(ctx, `INSERT INTO items (id, template_id, owner_id, location, tab, slot, quantity, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)`,
			id, template, owner, location, tab, inSlot, quantity, at); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO item_events (item_id, kind, quantity_delta, cause_type, occurred_at, created_at, updated_at) VALUES ($1, 'created', 1, 'drop', $2, $2, $2)`, id, at); err != nil {
			t.Fatal(err)
		}
		if quantity == 0 {
			if _, err := pool.Exec(ctx, `INSERT INTO item_events (item_id, kind, quantity_delta, cause_type, occurred_at, created_at, updated_at) VALUES ($1, 'discarded', -1, 'player', $2, $2, $2)`, id, at); err != nil {
				t.Fatal(err)
			}
		}
		return id
	}
	before := []string{"wooden_sword", "short_bow", "iron_sword", "hunting_bow", "leather_cap", "leather_vest"}
	for k, template := range before {
		add(template, "inventory", k)
	}
	add("wooden_sword", "destroyed", 0)
	add("red_potion", "inventory", 0)
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}

	mismatches, err := itemtest.CheckInvariants(ctx, pool, data)
	if err != nil || len(mismatches) > 0 {
		t.Fatalf("invariants: %v %v", mismatches, err)
	}
	// Frozen: a range retuned since never changes what was rolled.
	lowest := map[string][2]int{
		"wooden_sword": {10, 0}, "short_bow": {9, 0}, "iron_sword": {16, 0}, "hunting_bow": {14, 0}, "leather_cap": {0, 3}, "leather_vest": {0, 5},
	}
	rows, err := pool.Query(ctx, `SELECT i.template_id, r.physical_attack, r.physical_defense, r.created_at FROM item_rolls r JOIN items i ON i.id = r.item_id`)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for rows.Next() {
		var template string
		var attack, defense int
		var created time.Time
		if err := rows.Scan(&template, &attack, &defense, &created); err != nil {
			t.Fatal(err)
		}
		if want := lowest[template]; attack != want[0] || defense != want[1] || !created.Equal(at) {
			t.Errorf("%s: attack %d defense %d at %v, want %v at the item's creation", template, attack, defense, created, want)
		}
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if n != len(before)+1 {
		t.Fatalf("%d rolls, want one per equipment item, destroyed too", n)
	}
}

// A roll from before the stat system keeps its stats under their new names, and gives none of the new ones.
func TestARollKeepsItsStatsUnderTheirNewNames(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.MigrationsBefore(migrations.FS, migrations.Table, "00010_stat_rolls.sql")(ctx, pool); err != nil {
		t.Fatal(err)
	}
	id, at := uuidv7.New(), time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO items (id, template_id, owner_id, location, tab, slot, quantity, created_at, updated_at) VALUES ($1, 'leather_cap', $2, 'inventory', 'equip', 0, 1, $3, $3)`, id, uuidv7.New(), at); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO item_rolls (item_id, attack, defense, str, dex, "int", agi, vit, accuracy, evasion, crit_rate, crit_damage, max_hp, max_mp, created_at, updated_at) VALUES ($1, 2, 4, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, $2, $2)`, id, at); err != nil {
		t.Fatal(err)
	}
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var attack, defense, str, magicAttack, penetration, moveSpeed int
	if err := pool.QueryRow(ctx, `SELECT physical_attack, physical_defense, str, magic_attack, penetration, move_speed FROM item_rolls WHERE item_id = $1`, id).
		Scan(&attack, &defense, &str, &magicAttack, &penetration, &moveSpeed); err != nil {
		t.Fatal(err)
	}
	if attack != 2 || defense != 4 || str != 1 || magicAttack != 0 || penetration != 0 || moveSpeed != 0 {
		t.Fatalf("roll: physical attack %d, physical defense %d, STR %d, new stats %d %d %d", attack, defense, str, magicAttack, penetration, moveSpeed)
	}
}

// A swap moves the new item into the equip slot before the worn one leaves it, so one item per equip slot holds at commit, not row by row.
func TestOneItemPerEquipSlotHoldsAtCommit(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	owner, worn, next := uuidv7.New(), uuidv7.New(), uuidv7.New()
	if _, err := pool.Exec(ctx, `INSERT INTO items (id, template_id, owner_id, location, tab, slot, equip_slot, quantity, created_at, updated_at) VALUES
		($1, 'wooden_sword', $3, 'equipped', NULL, NULL, 'weapon', 1, now(), now()),
		($2, 'iron_sword', $3, 'inventory', 'equip', 0, NULL, 1, now(), now())`, worn, next, owner); err != nil {
		t.Fatal(err)
	}
	type change struct {
		sql string
		id  uuid.UUID
	}
	const (
		wear    = `UPDATE items SET location = 'equipped', tab = NULL, slot = NULL, equip_slot = 'weapon' WHERE id = $1`
		takeOff = `UPDATE items SET location = 'inventory', tab = 'equip', slot = 0, equip_slot = NULL WHERE id = $1`
	)
	commit := func(changes ...change) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		for _, c := range changes {
			if _, err := tx.Exec(ctx, c.sql, c.id); err != nil {
				return err
			}
		}
		return tx.Commit(ctx)
	}
	t.Run("two worn in one slot fail at commit", func(t *testing.T) {
		err := commit(change{wear, next})
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.ConstraintName != "one_item_per_equip_slot" {
			t.Fatalf("err = %v, want one_item_per_equip_slot", err)
		}
	})
	t.Run("a swap commits", func(t *testing.T) {
		if err := commit(change{wear, next}, change{takeOff, worn}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("a worn item names its slot, and only a worn one", func(t *testing.T) {
		for _, c := range []change{
			{`UPDATE items SET equip_slot = NULL WHERE id = $1`, next},
			{`UPDATE items SET equip_slot = 'hat' WHERE id = $1`, worn},
		} {
			if err := commit(c); err == nil {
				t.Fatalf("%s committed", c.sql)
			}
		}
	})
}
