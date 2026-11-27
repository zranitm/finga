// Package dbtest is the Postgres the repository layer's tests share with the use cases' that run on it: one template database migrated with the whole sequence, a database of its own for each test, started the first time one is asked for.
package dbtest

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/migration"
	"finga/pkg/pgtest"
)

var (
	once    sync.Once
	cluster *pgtest.Cluster
	failed  error
)

// start brings the cluster up on the first call, so a package whose tests never touch the database needs no Postgres.
func start() {
	once.Do(func() {
		cluster, failed = pgtest.Start(context.Background(), pgtest.Migrations(migration.FS, migration.Table)) // the whole sequence, as the server runs on it
	})
	if failed != nil {
		panic(failed)
	}
}

// Run runs a package's tests, then releases the cluster if one started, and reports the exit code; a TestMain that wraps the run in a check of its own, goleak's, calls it.
func Run(m interface{ Run() int }) int {
	code := m.Run()
	if cluster != nil {
		_ = cluster.Close(context.Background())
	}
	return code
}

// Main is a package's TestMain: its tests, then the cluster released.
func Main(m *testing.M) { os.Exit(Run(m)) }

// DB is a fresh migrated database for t, and its pool.
func DB(t testing.TB) *pgxpool.Pool {
	t.Helper()
	start()
	return cluster.DB(t)
}

// URL is the connection string of a fresh migrated database for t, for a test that opens its pool itself.
func URL(t testing.TB) string {
	t.Helper()
	start()
	return cluster.URL(t)
}
