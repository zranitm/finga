package pgtest_test

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"

	"finga/pkg/dbx"
	"finga/pkg/pgtest"
)

const stampFn = `CREATE FUNCTION set_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at := now();
  RETURN NEW;
END
$$;`

func TestCheckTimestamps(t *testing.T) {
	const stamped = `CREATE TABLE t (
  id int PRIMARY KEY,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);`
	const trigger = `CREATE TRIGGER set_updated_at BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION set_updated_at();`
	tests := []struct {
		name string
		ddl  string
		want string // in the error; empty for none
	}{
		{"both stamps and the trigger pass", stamped + trigger, ""},
		{"a missing updated_at is reported", `CREATE TABLE t (id int, created_at timestamptz NOT NULL DEFAULT now());` + trigger, "t.updated_at is missing"},
		{"a nullable stamp is reported", `CREATE TABLE t (id int, created_at timestamptz DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());` + trigger, "t.created_at is timestamp with time zone DEFAULT now()"},
		{"a stamp without a default is reported", `CREATE TABLE t (id int, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL DEFAULT now());` + trigger, "t.created_at is timestamp with time zone NOT NULL,"},
		{"a stamp without a time zone is reported", `CREATE TABLE t (id int, created_at timestamp NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now());` + trigger, "t.created_at is timestamp without time zone"},
		{"a table without the trigger is reported", stamped, "t has no BEFORE UPDATE row trigger running set_updated_at()"},
		{"an AFTER UPDATE trigger is not enough", stamped + `CREATE TRIGGER late AFTER UPDATE ON t FOR EACH ROW EXECUTE FUNCTION set_updated_at();`, "t has no BEFORE UPDATE"},
		{"a trigger on one column is not enough", stamped + `CREATE TRIGGER narrow BEFORE UPDATE OF id ON t FOR EACH ROW EXECUTE FUNCTION set_updated_at();`, "t has no BEFORE UPDATE"},
		{"a disabled trigger is not enough", stamped + trigger + `ALTER TABLE t DISABLE TRIGGER set_updated_at;`, "t has no BEFORE UPDATE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			pool := cluster.DB(t)
			if _, err := pool.Exec(ctx, stampFn+tt.ddl); err != nil {
				t.Fatal(err)
			}
			err := pgtest.CheckTimestamps(ctx, pool, "set_updated_at", "notes")
			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("err = %v", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("err = %v, want one saying %q", err, tt.want)
			}
		})
	}
	t.Run("no table to check is an error", func(t *testing.T) {
		if err := pgtest.CheckTimestamps(context.Background(), cluster.DB(t), "set_updated_at", "notes"); err == nil {
			t.Fatal("no error")
		}
	})
}

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
		if got, err := dbx.TableExists(ctx, pool, table); err != nil || got != want {
			t.Fatalf("table %s exists = %v (%v), want %v", table, got, err, want)
		}
	}
	if err := pgtest.Migrations(fsys, "goose_t_version")(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if got, err := dbx.TableExists(ctx, pool, "b"); err != nil || !got {
		t.Fatalf("the rest did not apply: %v, %v", got, err)
	}
}
