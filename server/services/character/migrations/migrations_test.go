package migrations_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"finga/pkg/pgtest"
	"finga/pkg/uuidv7"
	"finga/services/character/migrations"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	// No template migrations: each test applies what it needs.
	cluster, err = pgtest.Start(ctx, nil)
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

const (
	timestamps = "00003_timestamps.sql"
	// a character's columns but the stamps, then $1 the ID and $2 the name
	columns = `id, account_id, name, class, level, exp, strength, dexterity, intelligence, agility, vitality, hp, mp, map_id, pos_x, pos_y`
	values  = `$1, $1, $2, 'warrior', 1, 0, 12, 5, 4, 5, 10, 204, 28, 'town', 416, 608`
)

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
		err := pool.QueryRow(ctx, `INSERT INTO characters (`+columns+`) VALUES (`+values+`)
			RETURNING created_at, updated_at, now()`, uuidv7.New(), "Ayla").Scan(&created, &updated, &now)
		if err != nil {
			t.Fatal(err)
		}
		if !created.Equal(now) || !updated.Equal(now) {
			t.Fatalf("created_at %v, updated_at %v, want both %v", created, updated, now)
		}
	})
	t.Run("an update moves updated_at, even when it names one, and leaves created_at", func(t *testing.T) {
		id, past := uuidv7.New(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if _, err := pool.Exec(ctx, `INSERT INTO characters (`+columns+`, created_at, updated_at) VALUES (`+values+`, $3, $3)`, id, "Robin", past); err != nil {
			t.Fatal(err)
		}
		err := pool.QueryRow(ctx, `UPDATE characters SET map_id = 'forest', updated_at = $2 WHERE id = $1
			RETURNING created_at, updated_at, now()`, id, past).Scan(&created, &updated, &now)
		if err != nil {
			t.Fatal(err)
		}
		if !created.Equal(past) || !updated.Equal(now) {
			t.Fatalf("created_at %v, updated_at %v; want %v and %v", created, updated, past, now)
		}
	})
}

// A checkpoint moves updated_at every 30 s, so no index may hold it, or no save could be a HOT update.
func TestTheAccountIndexOrdersByCreation(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	rows, err := pool.Query(ctx, `SELECT indexname, indexdef FROM pg_indexes WHERE tablename = 'characters'`)
	if err != nil {
		t.Fatal(err)
	}
	defs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([2]string, error) {
		var d [2]string
		return d, row.Scan(&d[0], &d[1])
	})
	if err != nil {
		t.Fatal(err)
	}
	byAccount := false
	for _, d := range defs {
		if strings.Contains(d[1], "updated_at") {
			t.Errorf("%s holds updated_at: %s", d[0], d[1])
		}
		byAccount = byAccount || d[0] == "characters_by_account" && strings.HasSuffix(d[1], "(account_id, created_at)")
	}
	if !byAccount {
		t.Fatalf("no characters_by_account on (account_id, created_at) among %v", defs)
	}
}

// A character from before the timestamps migration was created no later than its last save.
func TestCreatedAtIsBackfilledFromUpdatedAt(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.MigrationsBefore(migrations.FS, migrations.Table, timestamps)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	id, saved := uuidv7.New(), time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `INSERT INTO characters (`+columns+`, updated_at) VALUES (`+values+`, $3)`, id, "Ayla", saved); err != nil {
		t.Fatal(err)
	}
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var created, updated time.Time
	if err := pool.QueryRow(ctx, `SELECT created_at, updated_at FROM characters WHERE id = $1`, id).Scan(&created, &updated); err != nil {
		t.Fatal(err)
	}
	if !created.Equal(saved) || !updated.Equal(saved) {
		t.Fatalf("created_at %v, updated_at %v, want both %v", created, updated, saved)
	}
}
