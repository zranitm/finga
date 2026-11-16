package migration_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
	"gopkg.in/yaml.v3"

	"finga/internal/migration"
	"finga/pkg/dbx"
)

// sqlcSchemas is each module's migration files as sqlc.yaml gives them to its queries, keyed by module.
func sqlcSchemas(t *testing.T) map[string][]string {
	t.Helper()
	raw, err := os.ReadFile("../../sqlc.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		SQL []struct {
			Schema  []string `yaml:"schema"`
			Queries string   `yaml:"queries"`
		} `yaml:"sql"`
	}
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	out := map[string][]string{}
	for _, e := range cfg.SQL {
		module := path.Base(path.Dir(e.Queries)) // internal/repository/<module>/queries
		for _, s := range e.Schema {
			out[module] = append(out[module], path.Base(s))
		}
	}
	return out
}

// Each module's queries see only its own files, named for it: the sequence's every file is one module's, listed once.
func TestSqlcGivesEachModuleItsOwnFiles(t *testing.T) {
	files, err := fs.Glob(migration.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for module, own := range sqlcSchemas(t) {
		for _, f := range own {
			if !strings.HasPrefix(f[6:], module) {
				t.Errorf("sqlc gives %s's queries %s, another module's", module, f)
			}
		}
		listed = append(listed, own...)
	}
	slices.Sort(listed)
	if !slices.Equal(listed, files) {
		t.Fatalf("sqlc.yaml lists %v; the sequence is %v", listed, files)
	}
}

// A module's files create its tables and no other's, so the schema its queries are checked against holds only its own.
func TestAModulesFilesCreateOnlyItsTables(t *testing.T) {
	schemas := sqlcSchemas(t)
	tests := []struct {
		module  string
		owns    []string
		notOwns []string
	}{
		{"account", []string{"accounts", "sessions"}, []string{"characters", "items"}},
		{"character", []string{"characters"}, []string{"accounts", "items"}},
		{"item", []string{"items", "item_events", "outbox", "idempotency_keys"}, []string{"accounts", "characters"}},
	}
	for _, tt := range tests {
		t.Run(tt.module, func(t *testing.T) {
			ctx := context.Background()
			own := fstest.MapFS{}
			for _, f := range schemas[tt.module] {
				body, err := fs.ReadFile(migration.FS, f)
				if err != nil {
					t.Fatal(err)
				}
				own[f] = &fstest.MapFile{Data: body}
			}
			pool := cluster.DB(t)
			if _, err := dbx.Migrate(ctx, pool, own, migration.Table); err != nil {
				t.Fatal(err)
			}
			for _, table := range tt.owns {
				if !exists(t, pool, table) {
					t.Fatalf("no %s", table)
				}
			}
			for _, table := range tt.notOwns {
				if exists(t, pool, table) {
					t.Fatalf("%s's files created %s", tt.module, table)
				}
			}
		})
	}
}

// The sequence rolls back newest first, every Down section running, and leaves citext, which two modules' tables use and none of their files drops.
func TestTheSequenceRollsBackAndLeavesCitext(t *testing.T) {
	ctx := context.Background()
	pool := migratedDB(t)
	files, err := fs.Glob(migration.FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range slices.Backward(files) {
		body, err := fs.ReadFile(migration.FS, name)
		if err != nil {
			t.Fatal(err)
		}
		_, down, ok := strings.Cut(string(body), "-- +goose Down")
		if !ok {
			t.Fatalf("%s has no Down section", name)
		}
		if _, err := pool.Exec(ctx, down); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for _, table := range []string{"accounts", "characters", "items"} {
		if exists(t, pool, table) {
			t.Fatalf("%s is left after the rollback", table)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_extension WHERE extname = 'citext'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("citext: %d, %v", n, err)
	}
}

// Every file is NNNNN_module_name.sql, numbered one after another, with an Up and a Down section.
func TestEveryFileIsNumberedAndSQL(t *testing.T) {
	entries, err := fs.ReadDir(migration.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("no migrations embedded")
	}
	for i, e := range entries {
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			if want := fmt.Sprintf("%05d_", i+1); !strings.HasPrefix(name, want) || !strings.HasSuffix(name, ".sql") {
				t.Fatalf("%q is not %sname.sql", name, want)
			}
			body, err := fs.ReadFile(migration.FS, name)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), "-- +goose Up") || !strings.Contains(string(body), "-- +goose Down") {
				t.Fatalf("%q lacks an Up or a Down section", name)
			}
		})
	}
}

func exists(t *testing.T, pool *pgxpool.Pool, table string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}
