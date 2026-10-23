package pgtest_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/pkg/pgtest"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, func(ctx context.Context, pool *pgxpool.Pool) error {
		// A stand-in for the real migrations: the template gets one table.
		_, err := pool.Exec(ctx, `CREATE TABLE notes (id int PRIMARY KEY, text text NOT NULL)`)
		return err
	})
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

func TestEachTestGetsItsOwnDatabase(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		text string
	}{
		{"first writer", "one"},
		{"second writer", "two"},
		{"third writer", "three"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := cluster.DB(t)
			// The template's table is there, and empty: no other test's row.
			var n int
			if err := pool.QueryRow(ctx, `SELECT count(*) FROM notes`).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Fatalf("a fresh database holds %d rows", n)
			}
			// The same primary key in every test: it would conflict in a shared database.
			if _, err := pool.Exec(ctx, `INSERT INTO notes VALUES (1, $1)`, tt.text); err != nil {
				t.Fatal(err)
			}
			var text string
			if err := pool.QueryRow(ctx, `SELECT text FROM notes WHERE id = 1`).Scan(&text); err != nil {
				t.Fatal(err)
			}
			if text != tt.text {
				t.Fatalf("read %q, want %q", text, tt.text)
			}
		})
	}
}

func TestDatabasesAreDistinct(t *testing.T) {
	a, b := cluster.DB(t), cluster.DB(t)
	if a.Config().ConnConfig.Database == b.Config().ConnConfig.Database {
		t.Fatalf("two DB calls share database %q", a.Config().ConnConfig.Database)
	}
	if cluster.URL(t) == "" {
		t.Fatal("URL is empty")
	}
}
