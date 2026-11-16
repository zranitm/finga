package migration_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/migration"
	"finga/pkg/pgtest"
)

var cluster *pgtest.Cluster

// TestMain starts an empty template: some tests migrate only some files, and the library tables' check compares with an empty database.
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

// migratedDB is a fresh database with the whole sequence applied.
func migratedDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := cluster.DB(t)
	if err := pgtest.Migrations(migration.FS, migration.Table)(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}
