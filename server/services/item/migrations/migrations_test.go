package migrations_test

import (
	"context"
	"io/fs"
	"os"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/pkg/idempotency"
	"finga/pkg/outbox"
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
	for _, ddl := range []string{outbox.Schema, idempotency.Schema} {
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

// shape lists a table's columns, in order, and its index definitions.
func shape(t *testing.T, pool *pgxpool.Pool, table string) []string {
	t.Helper()
	ctx := context.Background()
	var out []string
	rows, err := pool.Query(ctx, `
		SELECT column_name || ' ' || data_type || ' ' || is_nullable
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
