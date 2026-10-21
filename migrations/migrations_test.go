package migrations_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/migrations"
	"finga/pkg/pgtest"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	// The cluster's template database gets no migrations here, so each test
	// starts from an empty database and applies them itself.
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

	t.Run("applies every migration to an empty database", func(t *testing.T) {
		n, err := migrations.Apply(ctx, pool)
		if err != nil {
			t.Fatal(err)
		}
		if n < 1 {
			t.Fatalf("applied %d migrations, want at least 1", n)
		}
		if !hasExtension(t, pool, "citext") {
			t.Fatal("citext is not enabled")
		}
	})

	t.Run("a second run changes nothing", func(t *testing.T) {
		n, err := migrations.Apply(ctx, pool)
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("second run applied %d migrations, want 0", n)
		}
	})
}

func TestEveryFileIsNumberedAndSQL(t *testing.T) {
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no migrations embedded")
	}
	for _, e := range entries {
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			if len(name) < 7 || name[5] != '_' || name[len(name)-4:] != ".sql" {
				t.Fatalf("%q is not NNNNN_name.sql", name)
			}
			for _, c := range name[:5] {
				if c < '0' || c > '9' {
					t.Fatalf("%q does not start with five digits", name)
				}
			}
		})
	}
}

func hasExtension(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_extension WHERE extname = $1`, name).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}
