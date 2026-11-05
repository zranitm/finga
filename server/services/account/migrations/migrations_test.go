package migrations_test

import (
	"context"
	"os"
	"testing"
	"time"

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
		err := pool.QueryRow(ctx, `INSERT INTO accounts (id, username, password_hash) VALUES ($1, 'alice', 'h')
			RETURNING created_at, updated_at, now()`, uuidv7.New()).Scan(&created, &updated, &now)
		if err != nil {
			t.Fatal(err)
		}
		if !created.Equal(now) || !updated.Equal(now) {
			t.Fatalf("created_at %v, updated_at %v, want both %v", created, updated, now)
		}
	})
	t.Run("an update moves updated_at, even when it names one, and leaves created_at", func(t *testing.T) {
		id, past := uuidv7.New(), time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, username, password_hash, created_at, updated_at)
			VALUES ($1, 'bob', 'h', $2, $2)`, id, past); err != nil {
			t.Fatal(err)
		}
		err := pool.QueryRow(ctx, `UPDATE accounts SET settings = '{"control_preset":"wasd"}', updated_at = $2 WHERE id = $1
			RETURNING created_at, updated_at, now()`, id, past).Scan(&created, &updated, &now)
		if err != nil {
			t.Fatal(err)
		}
		if !created.Equal(past) || !updated.Equal(now) {
			t.Fatalf("created_at %v, updated_at %v; want %v and %v", created, updated, past, now)
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
