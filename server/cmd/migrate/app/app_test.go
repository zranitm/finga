package app_test

import (
	"context"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3/lock"

	"finga/cmd/migrate/app"
	"finga/pkg/dbx"
	"finga/pkg/pgtest"
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
	})
}

// Two migrate runs at once, as two deploys would start them, take turns: both pass, and each migration runs once.
func TestTwoRunsAtOnceTakeTurns(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	results := make(chan map[string]int, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			applied, err := app.Apply(ctx, pool)
			results <- applied
			errs <- err
		}()
	}
	total := map[string]int{}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		for service, n := range <-results {
			total[service] += n
		}
	}
	for _, s := range app.Sets() {
		files, err := fs.Glob(s.FS, "*.sql")
		if err != nil {
			t.Fatal(err)
		}
		if total[s.Service] != len(files) {
			t.Fatalf("%s: %d migrations run across both, want each of its %d once", s.Service, total[s.Service], len(files))
		}
	}
}

// While another run holds the migration lock, Apply waits and changes nothing until it has the lock.
func TestARunWaitsForTheMigrationLock(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, lock.DefaultLockID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := app.Apply(ctx, pool)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("Apply finished while another run held the lock: %v", err)
	case <-time.After(2 * time.Second):
	}
	if exists(t, pool, find(t, "account").Table) {
		t.Fatal("Apply migrated before it had the lock")
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, lock.DefaultLockID); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
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
	var ok bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&ok); err != nil {
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
