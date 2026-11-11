package pgtest

import (
	"context"
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
