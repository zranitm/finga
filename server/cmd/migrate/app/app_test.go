package app_test

import (
	"context"
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pressly/goose/v3/lock"

	"finga/cmd/migrate/app"
	"finga/internal/migration"
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
	t.Parallel()
	ctx := context.Background()
	pool := cluster.DB(t)

	t.Run("the sequence applies to an empty database", func(t *testing.T) {
		applied, err := app.Apply(ctx, pool)
		if err != nil {
			t.Fatal(err)
		}
		if files := count(t); applied != files {
			t.Fatalf("applied %d migrations of %d", applied, files)
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
		if applied != 0 {
			t.Fatalf("applied %d on the second run", applied)
		}
	})
	t.Run("its versions are in goose's one table", func(t *testing.T) {
		var latest int
		if err := pool.QueryRow(ctx, `SELECT max(version_id) FROM `+migration.Table).Scan(&latest); err != nil || latest != count(t) {
			t.Fatalf("latest version %d, %v; want %d", latest, err, count(t))
		}
	})
}

// Two migrate runs at once, as two deploys would start them, take turns: both pass, and each migration runs once.
func TestTwoRunsAtOnceTakeTurns(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	pool := cluster.DB(t)
	results := make(chan int, 2)
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			applied, err := app.Apply(ctx, pool)
			results <- applied
			errs <- err
		}()
	}
	total := 0
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
		total += <-results
	}
	if files := count(t); total != files {
		t.Fatalf("%d migrations run across both, want each of the %d once", total, files)
	}
}

// While another run holds the migration lock, Apply waits and changes nothing until it has the lock.
func TestARunWaitsForTheMigrationLock(t *testing.T) {
	t.Parallel()
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
	if exists(t, pool, migration.Table) {
		t.Fatal("Apply migrated before it had the lock")
	}
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, lock.DefaultLockID); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// count is how many files the sequence holds.
func count(t *testing.T) int {
	t.Helper()
	files, err := fs.Glob(migration.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	return len(files)
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
