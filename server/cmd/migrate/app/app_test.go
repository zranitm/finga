package app_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/cmd/migrate/app"
	"finga/pkg/dbx"
	"finga/pkg/pgtest"
	"finga/pkg/uuidv7"
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

func TestApply(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)

	t.Run("every service's set applies to an empty database", func(t *testing.T) {
		applied, err := app.Apply(ctx, pool)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range app.Sets() {
			if applied[s.Service] < 1 {
				t.Fatalf("%s applied %d migrations", s.Service, applied[s.Service])
			}
		}
		for _, table := range []string{"accounts", "sessions", "characters", "items", "item_events", "outbox", "idempotency_keys"} {
			if !exists(t, pool, table) {
				t.Fatalf("no %s table", table)
			}
		}
		if !hasExtension(t, pool, "citext") {
			t.Fatal("citext is not enabled")
		}
	})
	t.Run("a second run applies nothing", func(t *testing.T) {
		applied, err := app.Apply(ctx, pool)
		if err != nil {
			t.Fatal(err)
		}
		for service, n := range applied {
			if n != 0 {
				t.Fatalf("%s applied %d on the second run", service, n)
			}
		}
	})
	t.Run("each set records its versions in its own table", func(t *testing.T) {
		for _, s := range app.Sets() {
			if s.Table != "goose_"+s.Service+"_version" || !exists(t, pool, s.Table) {
				t.Fatalf("%s: version table %q", s.Service, s.Table)
			}
		}
		if exists(t, pool, app.LegacyTable) {
			t.Fatalf("%s exists", app.LegacyTable)
		}
	})
}

func TestALegacyDatabaseIsRefused(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE goose_db_version (id serial PRIMARY KEY, version_id bigint NOT NULL, is_applied boolean NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	_, err := app.Apply(ctx, pool)
	if !errors.Is(err, app.ErrLegacy) || !strings.Contains(err.Error(), "make down") {
		t.Fatalf("err = %v, want ErrLegacy naming make down", err)
	}
	for _, s := range app.Sets() {
		if exists(t, pool, s.Table) {
			t.Fatalf("%s was created although the database was refused", s.Table)
		}
	}
}

// An ID minted before every ID was a UUIDv7 fails each strict parse, which would lock its player out.
func TestADatabaseHoldingOlderIDsIsRefused(t *testing.T) {
	const v4 = "6f1c2a3b-4d5e-4f60-8a71-92b3c4d5e6f7"
	ctx := context.Background()
	refused := func(t *testing.T, err error, column string) {
		t.Helper()
		if !errors.Is(err, app.ErrNotV7) || !strings.Contains(err.Error(), column+" holds "+v4) || !strings.Contains(err.Error(), "make down") {
			t.Fatalf("err = %v, want ErrNotV7 naming %s, its ID and make down", err, column)
		}
	}

	t.Run("before any migration runs", func(t *testing.T) {
		pool := cluster.DB(t)
		account := find(t, "account")
		if err := pgtest.MigrationsBefore(account.FS, account.Table, "00004_timestamps.sql")(ctx, pool); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO accounts (id, username, password_hash, created_at) VALUES ($1, 'alice', 'x', now())`, v4); err != nil {
			t.Fatal(err)
		}
		_, err := app.Apply(ctx, pool)
		refused(t, err, "accounts.id")
		var version int64
		if err := pool.QueryRow(ctx, `SELECT max(version_id) FROM `+account.Table).Scan(&version); err != nil || version != 3 {
			t.Fatalf("account version %d (%v), want 3: nothing applied", version, err)
		}
		if exists(t, pool, find(t, "character").Table) {
			t.Fatal("the character set ran although the database was refused")
		}
	})

	const character = `INSERT INTO characters (id, account_id, name, class, level, exp, strength, dexterity, intelligence, agility, vitality, hp, mp, map_id, pos_x, pos_y)
		VALUES ($2, $1, 'Ayla', 'warrior', 1, 0, 12, 5, 4, 5, 10, 204, 28, 'town', 416, 608)`
	const item = `INSERT INTO items (id, template_id, owner_id, location, tab, slot, quantity) VALUES ($1, 'red_potion', $2, 'inventory', 'use', 0, 1)`
	for _, tt := range []struct{ column, insert string }{{"characters.account_id", character}, {"items.id", item}} {
		t.Run("in "+tt.column, func(t *testing.T) {
			pool := cluster.DB(t)
			if _, err := app.Apply(ctx, pool); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, tt.insert, v4, uuidv7.New()); err != nil {
				t.Fatal(err)
			}
			_, err := app.Apply(ctx, pool)
			refused(t, err, tt.column)
		})
	}

	t.Run("a database of UUIDv7 IDs migrates", func(t *testing.T) {
		pool := cluster.DB(t)
		if _, err := app.Apply(ctx, pool); err != nil {
			t.Fatal(err)
		}
		for _, insert := range []string{character, item} {
			if _, err := pool.Exec(ctx, insert, uuidv7.New(), uuidv7.New()); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := app.Apply(ctx, pool); err != nil {
			t.Fatal(err)
		}
	})
}

func TestASetCreatesOnlyItsServicesTables(t *testing.T) {
	tests := []struct {
		service string
		owns    []string
		notOwns []string
	}{
		{"account", []string{"accounts", "sessions"}, []string{"characters", "items"}},
		{"character", []string{"characters"}, []string{"accounts", "items"}},
		{"item", []string{"items", "item_events", "outbox", "idempotency_keys"}, []string{"accounts", "characters"}},
	}
	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			ctx := context.Background()
			pool := cluster.DB(t)
			set := find(t, tt.service)
			if _, err := dbx.Migrate(ctx, pool, set.FS, set.Table); err != nil {
				t.Fatal(err)
			}
			for _, table := range tt.owns {
				if !exists(t, pool, table) {
					t.Fatalf("no %s", table)
				}
			}
			for _, table := range tt.notOwns {
				if exists(t, pool, table) {
					t.Fatalf("%s's set created %s", tt.service, table)
				}
			}
		})
	}
}

// citext belongs to two services, so rolling either back must leave it for the other.
func TestCitextSurvivesEitherRollback(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if _, err := app.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE sessions; DROP TABLE accounts; DELETE FROM goose_account_version WHERE version_id > 0`); err != nil {
		t.Fatal(err)
	}
	down := downSection(t, find(t, "account").FS, "00001_citext.sql")
	if _, err := pool.Exec(ctx, down); err != nil {
		t.Fatal(err)
	}
	if !hasExtension(t, pool, "citext") {
		t.Fatal("rolling the account service back dropped citext")
	}
}

func TestEveryFileIsNumberedAndSQL(t *testing.T) {
	for _, s := range app.Sets() {
		entries, err := fs.ReadDir(s.FS, ".")
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			t.Fatalf("%s: no migrations embedded", s.Service)
		}
		for _, e := range entries {
			name := e.Name()
			t.Run(s.Service+"/"+name, func(t *testing.T) {
				if len(name) < 7 || name[5] != '_' || !strings.HasSuffix(name, ".sql") {
					t.Fatalf("%q is not NNNNN_name.sql", name)
				}
				for _, c := range name[:5] {
					if c < '0' || c > '9' {
						t.Fatalf("%q does not start with five digits", name)
					}
				}
				body, err := fs.ReadFile(s.FS, name)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(body), "-- +goose Up") || !strings.Contains(string(body), "-- +goose Down") {
					t.Fatalf("%q lacks an Up or a Down section", name)
				}
			})
		}
	}
}

func find(t *testing.T, service string) app.Set {
	t.Helper()
	for _, s := range app.Sets() {
		if s.Service == service {
			return s
		}
	}
	t.Fatalf("no set for %s", service)
	return app.Set{}
}

func downSection(t *testing.T, fsys fs.FS, name string) string {
	t.Helper()
	body, err := fs.ReadFile(fsys, name)
	if err != nil {
		t.Fatal(err)
	}
	_, down, ok := strings.Cut(string(body), "-- +goose Down")
	if !ok {
		t.Fatalf("%s has no Down section", name)
	}
	return down
}

func exists(t *testing.T, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	ok, err := dbx.TableExists(context.Background(), pool, table)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func hasExtension(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_extension WHERE extname = $1`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}
