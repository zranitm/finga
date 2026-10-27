package dbx_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/pkg/dbx"
)

// twoFiles is a migration set in goose's format, as a service embeds it.
var twoFiles = fstest.MapFS{
	"00001_first.sql":  {Data: []byte("-- +goose Up\nCREATE TABLE first (id int PRIMARY KEY);\n-- +goose Down\nDROP TABLE first;\n")},
	"00002_second.sql": {Data: []byte("-- +goose Up\nCREATE TABLE second (id int PRIMARY KEY);\n-- +goose Down\nDROP TABLE second;\n")},
}

func TestMigrate(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)

	t.Run("applies every file to an empty database", func(t *testing.T) {
		n, err := dbx.Migrate(ctx, pool, twoFiles, "goose_shop_version")
		if err != nil {
			t.Fatal(err)
		}
		if n != 2 || !exists(t, pool, "first") || !exists(t, pool, "second") {
			t.Fatalf("applied %d", n)
		}
	})
	t.Run("a second run applies nothing", func(t *testing.T) {
		n, err := dbx.Migrate(ctx, pool, twoFiles, "goose_shop_version")
		if err != nil || n != 0 {
			t.Fatalf("applied %d, err %v", n, err)
		}
	})
	t.Run("the versions are in the named table, not goose's default", func(t *testing.T) {
		if !exists(t, pool, "goose_shop_version") {
			t.Fatal("no goose_shop_version")
		}
		if exists(t, pool, "goose_db_version") {
			t.Fatal("goose_db_version exists")
		}
		var latest int64
		if err := pool.QueryRow(ctx, `SELECT max(version_id) FROM goose_shop_version`).Scan(&latest); err != nil || latest != 2 {
			t.Fatalf("latest version %d, err %v", latest, err)
		}
	})
	t.Run("another table name is another set", func(t *testing.T) {
		other := fstest.MapFS{"00001_third.sql": {Data: []byte("-- +goose Up\nCREATE TABLE third (id int);\n-- +goose Down\nDROP TABLE third;\n")}}
		n, err := dbx.Migrate(ctx, pool, other, "goose_other_version")
		if err != nil || n != 1 || !exists(t, pool, "third") {
			t.Fatalf("applied %d, err %v", n, err)
		}
	})
	t.Run("a failing file is reported", func(t *testing.T) {
		bad := fstest.MapFS{"00001_bad.sql": {Data: []byte("-- +goose Up\nCREATE TABL oops;\n-- +goose Down\n")}}
		if _, err := dbx.Migrate(ctx, pool, bad, "goose_bad_version"); err == nil {
			t.Fatal("no error")
		}
	})
}

func TestTableExists(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE here (id int)`); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{"here": true, "nowhere": false} {
		got, err := dbx.TableExists(ctx, pool, name)
		if err != nil || got != want {
			t.Fatalf("%s: %v, err %v; want %v", name, got, err, want)
		}
	}
}

func exists(t *testing.T, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	ok, err := dbx.TableExists(context.Background(), pool, table)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}
