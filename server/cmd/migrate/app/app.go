// Package app is the migrate binary's composition root, shared with the end-to-end tests.
package app

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/internal/migration"
	"finga/pkg/dbx"
)

// Apply applies the sequence's pending migrations and reports how many ran; it holds the migration lock, so two runs at once take turns.
func Apply(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	var applied int
	err := dbx.Locked(ctx, pool, func() error {
		var err error
		applied, err = dbx.Migrate(ctx, pool, migration.FS, migration.Table)
		return err
	})
	return applied, err
}
