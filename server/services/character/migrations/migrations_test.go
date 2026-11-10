package migrations_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

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
	appStamps  = "00005_app_stamps.sql"
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

// A character's stat columns hold the points spent, never negative, and it starts with its free reset.
func TestPointsAreNeverNegative(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	insert := func(name string, str, unspent int) error {
		_, err := pool.Exec(ctx, `INSERT INTO characters (id, account_id, name, class, level, exp, str_points, dex_points, int_points, agi_points, vit_points, unspent_points, hp, mp, map_id, pos_x, pos_y, created_at, updated_at)
			VALUES ($1, $1, $2, 'warrior', 3, 0, $3, 0, 0, 0, 0, $4, 184, 32, 'town', 416, 608, now(), now())`, uuidv7.New(), name, str, unspent)
		return err
	}
	if err := insert("Ayla", 5, 5); err != nil {
		t.Fatal(err)
	}
	var used bool
	if err := pool.QueryRow(ctx, `SELECT free_reset_used FROM characters WHERE name = 'Ayla'`).Scan(&used); err != nil || used {
		t.Fatalf("free_reset_used %v, %v; want false", used, err)
	}
	for _, c := range []struct{ str, unspent int }{{-1, 0}, {0, -1}} {
		if err := insert("Robin", c.str, c.unspent); err == nil || !strings.Contains(err.Error(), "points_not_negative") {
			t.Fatalf("points %+v stored: %v", c, err)
		}
	}
}
