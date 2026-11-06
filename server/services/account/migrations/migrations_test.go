package migrations_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/pkg/pgtest"
	"finga/pkg/uuidv7"
	"finga/services/account/migrations"
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

// Rows from before the timestamps migration get the best times known.
func TestTimestampsAreBackfilled(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if err := pgtest.MigrationsBefore(migrations.FS, migrations.Table, timestamps)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	account := uuidv7.New()
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, username, password_hash, created_at) VALUES ($1, 'alice', 'h', $2)`, account, t0); err != nil {
		t.Fatal(err)
	}
	fresh, rotated, revoked := uuidv7.New(), uuidv7.New(), uuidv7.New()
	if _, err := pool.Exec(ctx, `INSERT INTO sessions (id, account_id, token_hash, created_at, expires_at, rotated_at, revoked_at) VALUES
		($1, $4, '\x01', $5, $5::timestamptz + interval '7 days', NULL, $5::timestamptz + interval '3 hours'),
		($2, $4, '\x02', $5, $5::timestamptz + interval '7 days', $5::timestamptz + interval '1 hour', $5::timestamptz + interval '2 hours'),
		($3, $4, '\x03', $5, $5::timestamptz + interval '7 days', NULL, NULL)`,
		revoked, rotated, fresh, account, t0); err != nil {
		t.Fatal(err)
	}
	if err := pgtest.Migrations(migrations.FS, migrations.Table)(ctx, pool); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name                     string
		table                    string
		id                       any
		wantCreated, wantUpdated time.Time
	}{
		{"an account was last changed when created", "accounts", account, t0, t0},
		{"an untouched session was last changed when created", "sessions", fresh, t0, t0},
		{"a revoked session was last changed when revoked", "sessions", revoked, t0, t0.Add(3 * time.Hour)},
		{"the latest of rotation and revocation wins", "sessions", rotated, t0, t0.Add(2 * time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var created, updated time.Time
			if err := pool.QueryRow(ctx, `SELECT created_at, updated_at FROM `+tt.table+` WHERE id = $1`, tt.id).Scan(&created, &updated); err != nil {
				t.Fatal(err)
			}
			if !created.Equal(tt.wantCreated) || !updated.Equal(tt.wantUpdated) {
				t.Fatalf("created_at %v, updated_at %v; want %v and %v", created, updated, tt.wantCreated, tt.wantUpdated)
			}
		})
	}
}
