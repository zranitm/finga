package pgtest_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/pkg/pgtest"
)

func TestMigrationsBefore(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	fsys := fstest.MapFS{
		"00001_a.sql": {Data: []byte("-- +goose Up\nCREATE TABLE a (id int);\n")},
		"00002_b.sql": {Data: []byte("-- +goose Up\nCREATE TABLE b (id int);\n")},
	}
	if err := pgtest.MigrationsBefore(fsys, "goose_t_version", "00002_b.sql")(ctx, pool); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string]bool{"a": true, "b": false} {
		if got := tableExists(t, pool, table); got != want {
			t.Fatalf("table %s exists = %v, want %v", table, got, want)
		}
	}
	if err := pgtest.Migrations(fsys, "goose_t_version")(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if !tableExists(t, pool, "b") {
		t.Fatal("the rest did not apply")
	}
}

func tableExists(t *testing.T, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}
