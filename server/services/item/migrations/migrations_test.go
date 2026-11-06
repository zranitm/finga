package migrations_test

import (
	"context"
	"io/fs"
	"os"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/pkg/idempotency/idempotencypg"
	"finga/pkg/outbox/outboxpg"
	"finga/pkg/pgtest"
	"finga/pkg/uuidv7"
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

// The outbox and idempotency tables here must match those packages' own Schema.
func TestTheLibraryTablesMatchTheirSchemas(t *testing.T) {
	ctx := context.Background()
	migrated := cluster.DB(t)
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, migrated); err != nil {
		t.Fatal(err)
	}
	reference := cluster.DB(t)
	for _, ddl := range []string{outboxpg.Schema, idempotencypg.Schema} {
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

const timestamps = "00004_timestamps.sql"

func TestEveryTableHasItsTimestamps(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := pgtest.CheckTimestamps(ctx, pool, "set_updated_at", migrations.Table); err != nil {
		t.Fatal(err)
	}
}

// Each statement is its own transaction, so now() is that statement's time.
func TestTheDatabaseStampsInsertsAndUpdates(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var created, updated, now time.Time
	t.Run("an insert takes both from the clock", func(t *testing.T) {
		err := pool.QueryRow(ctx, `INSERT INTO items (id, template_id, owner_id, location, tab, slot, quantity)
			VALUES ($1, 'red_potion', $2, 'inventory', 'use', 0, 5) RETURNING created_at, updated_at, now()`,
			uuidv7.New(), uuidv7.New()).Scan(&created, &updated, &now)
		if err != nil {
			t.Fatal(err)
		}
		if !created.Equal(now) || !updated.Equal(now) {
			t.Fatalf("created_at %v, updated_at %v, want both %v", created, updated, now)
		}
	})
	t.Run("an update moves updated_at, even when it names one, and leaves created_at", func(t *testing.T) {
		id, past := uuidv7.New(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if _, err := pool.Exec(ctx, `INSERT INTO items (id, template_id, owner_id, location, tab, slot, quantity, created_at, updated_at)
			VALUES ($1, 'red_potion', $2, 'inventory', 'use', 1, 5, $3, $3)`, id, uuidv7.New(), past); err != nil {
			t.Fatal(err)
		}
		err := pool.QueryRow(ctx, `UPDATE items SET quantity = 4, updated_at = $2 WHERE id = $1
			RETURNING created_at, updated_at, now()`, id, past).Scan(&created, &updated, &now)
		if err != nil {
			t.Fatal(err)
		}
		if !created.Equal(past) || !updated.Equal(now) {
			t.Fatalf("created_at %v, updated_at %v; want %v and %v", created, updated, past, now)
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
