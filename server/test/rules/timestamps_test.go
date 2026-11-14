// Every table has created_at and updated_at as timestamptz NOT NULL that nothing in the database fills: the app stamps both.

package rules_test

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	migrate "finga/cmd/migrate/app"
	"finga/pkg/pgtest"
)

var cluster *pgtest.Cluster

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

const stampShape = "timestamp with time zone NOT NULL"

// A row per table and stamp: the column's shape, and the BEFORE INSERT or UPDATE row triggers that could fill it.
const stampsSQL = `
SELECT c.relname, col.name,
  coalesce(format_type(a.atttypid, a.atttypmod)
    || CASE WHEN a.attnotnull THEN ' NOT NULL' ELSE '' END
    || coalesce(' DEFAULT ' || pg_get_expr(d.adbin, d.adrelid), ''), 'missing'),
  (SELECT string_agg(t.tgname, ', ' ORDER BY t.tgname) FROM pg_trigger t
    WHERE t.tgrelid = c.oid AND NOT t.tgisinternal AND t.tgtype & 3 = 3 AND t.tgtype & 20 <> 0)
FROM pg_class c
CROSS JOIN unnest(ARRAY['created_at', 'updated_at']) AS col(name)
LEFT JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = col.name AND NOT a.attisdropped
LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE c.relnamespace = to_regnamespace(current_schema()) AND c.relkind IN ('r', 'p') AND c.relname <> ALL ($1)
ORDER BY c.relname, col.name`

// unstamped lists what keeps each table but skip from stamping as the app does, and how many tables it read.
func unstamped(ctx context.Context, pool *pgxpool.Pool, skip ...string) (problems []string, tables int, err error) {
	rows, err := pool.Query(ctx, stampsSQL, append([]string{}, skip...))
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var table, column, shape string
		var triggers *string
		if err := rows.Scan(&table, &column, &shape, &triggers); err != nil {
			return nil, 0, err
		}
		if shape != stampShape {
			problems = append(problems, fmt.Sprintf("%s.%s is %s, want %s", table, column, shape, stampShape))
		}
		if triggers != nil && !seen[table] {
			problems = append(problems, fmt.Sprintf("%s has BEFORE row triggers: %s", table, *triggers))
		}
		seen[table] = true
	}
	return problems, len(seen), rows.Err()
}

// The check reports each way a stamp could be missing or filled by the database, and passes a table the app stamps.
func TestTheTimestampCheckFindsEveryUnstampedTable(t *testing.T) {
	const stampFn = `CREATE FUNCTION set_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END
$$;`
	const stamped = `CREATE TABLE t (
  id int PRIMARY KEY,
  created_at timestamptz NOT NULL,
  updated_at timestamptz NOT NULL
);`
	for _, tt := range []struct {
		name string
		ddl  string
		want []string
	}{
		{"both stamps without a default pass", stamped, nil},
		{"a trigger that cannot change the row passes", stamped + `CREATE TRIGGER late AFTER UPDATE ON t FOR EACH ROW EXECUTE FUNCTION set_updated_at();`, nil},
		{"a missing updated_at is reported", `CREATE TABLE t (id int, created_at timestamptz NOT NULL);`, []string{"t.updated_at is missing, want " + stampShape}},
		{"a nullable stamp is reported", `CREATE TABLE t (id int, created_at timestamptz, updated_at timestamptz NOT NULL);`, []string{"t.created_at is timestamp with time zone, want " + stampShape}},
		{"a default is reported", `CREATE TABLE t (id int, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL);`, []string{"t.created_at is timestamp with time zone NOT NULL DEFAULT now(), want " + stampShape}},
		{"a stamp without a time zone is reported", `CREATE TABLE t (id int, created_at timestamp NOT NULL, updated_at timestamptz NOT NULL);`, []string{"t.created_at is timestamp without time zone NOT NULL, want " + stampShape}},
		{"a BEFORE UPDATE row trigger is reported", stamped + `CREATE TRIGGER set_updated_at BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION set_updated_at();`, []string{"t has BEFORE row triggers: set_updated_at"}},
		{"a disabled one too", stamped + `CREATE TRIGGER fill BEFORE INSERT ON t FOR EACH ROW EXECUTE FUNCTION set_updated_at(); ALTER TABLE t DISABLE TRIGGER fill;`, []string{"t has BEFORE row triggers: fill"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			pool := cluster.DB(t)
			if _, err := pool.Exec(ctx, stampFn+tt.ddl+`CREATE TABLE skipped (id int);`); err != nil {
				t.Fatal(err)
			}
			got, tables, err := unstamped(ctx, pool, "skipped")
			if err != nil || tables != 1 || !slices.Equal(got, tt.want) {
				t.Fatalf("got %q from %d tables (%v), want %q from 1", got, tables, err, tt.want)
			}
		})
	}
}

// The modules share one database, migrated as cmd/migrate does; every table but goose's version tables is stamped by the app.
func TestEveryTableHasItsTimestamps(t *testing.T) {
	ctx := t.Context()
	pool := cluster.DB(t)
	if _, err := migrate.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var versions []string
	for _, s := range migrate.Sets() {
		versions = append(versions, s.Table)
	}
	problems, tables, err := unstamped(ctx, pool, versions...)
	if err != nil || tables == 0 {
		t.Fatalf("read %d tables (%v): is the query broken?", tables, err)
	}
	for _, p := range problems {
		t.Error(p)
	}
}
