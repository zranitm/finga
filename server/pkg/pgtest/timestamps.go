package pgtest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationsBefore applies the set's files that sort before file: the schema that file migrates from.
func MigrationsBefore(fsys fs.FS, table, file string) MigrateFunc {
	return func(ctx context.Context, pool *pgxpool.Pool) error {
		names, err := fs.Glob(fsys, "*.sql")
		if err != nil {
			return err
		}
		before := fstest.MapFS{}
		for _, name := range names {
			if name >= file {
				continue
			}
			b, err := fs.ReadFile(fsys, name)
			if err != nil {
				return err
			}
			before[name] = &fstest.MapFile{Data: b}
		}
		return Migrations(before, table)(ctx, pool)
	}
}

const stampShape = "timestamp with time zone NOT NULL DEFAULT now()"

// A row per table and stamp: the column's shape, and whether an enabled BEFORE UPDATE row trigger (tgtype 19), on all columns and without WHEN, runs $1.
const stampsSQL = `
SELECT c.relname, col.name,
  coalesce(format_type(a.atttypid, a.atttypmod)
    || CASE WHEN a.attnotnull THEN ' NOT NULL' ELSE '' END
    || coalesce(' DEFAULT ' || pg_get_expr(d.adbin, d.adrelid), ''), 'missing'),
  EXISTS (SELECT 1 FROM pg_trigger t
    WHERE t.tgrelid = c.oid AND t.tgfoid = to_regproc($1)::oid AND t.tgenabled <> 'D'
      AND t.tgtype & 19 = 19 AND t.tgattr::text = '' AND t.tgqual IS NULL)
FROM pg_class c
CROSS JOIN unnest(ARRAY['created_at', 'updated_at']) AS col(name)
LEFT JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = col.name AND NOT a.attisdropped
LEFT JOIN pg_attrdef d ON d.adrelid = a.attrelid AND d.adnum = a.attnum
WHERE c.relnamespace = to_regnamespace(current_schema()) AND c.relkind IN ('r', 'p') AND c.relname <> ALL ($2)
ORDER BY c.relname, col.name`

// CheckTimestamps checks that every table of the current schema, but those in skip, has created_at and
// updated_at as timestamptz NOT NULL DEFAULT now(), and a BEFORE UPDATE row trigger running fn.
func CheckTimestamps(ctx context.Context, pool *pgxpool.Pool, fn string, skip ...string) error {
	rows, err := pool.Query(ctx, stampsSQL, fn, append([]string{}, skip...))
	if err != nil {
		return fmt.Errorf("pgtest: timestamps: %w", err)
	}
	defer rows.Close()
	var problems []error
	tables := map[string]bool{}
	for rows.Next() {
		var table, column, shape string
		var trigger bool
		if err := rows.Scan(&table, &column, &shape, &trigger); err != nil {
			return fmt.Errorf("pgtest: timestamps: %w", err)
		}
		if shape != stampShape {
			problems = append(problems, fmt.Errorf("%s.%s is %s, want %s", table, column, shape, stampShape))
		}
		if !trigger && !tables[table] {
			problems = append(problems, fmt.Errorf("%s has no BEFORE UPDATE row trigger running %s()", table, fn))
		}
		tables[table] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("pgtest: timestamps: %w", err)
	}
	if len(tables) == 0 {
		return errors.New("pgtest: timestamps: no table to check")
	}
	return errors.Join(problems...)
}
