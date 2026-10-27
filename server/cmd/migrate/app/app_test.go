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
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	// The template gets no migrations: each test starts from an empty
	// database and applies what it needs.
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

// A service's set alone creates only that service's tables, so a query
// that reads another service's table has nothing to read.
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

// citext belongs to two services: rolling either one back to nothing must
// leave it for the other.
func TestCitextSurvivesEitherRollback(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if _, err := app.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE sessions; DROP TABLE accounts; DELETE FROM goose_account_version WHERE version_id > 0`); err != nil {
		t.Fatal(err)
	}
	// What the citext migration's Down runs.
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
