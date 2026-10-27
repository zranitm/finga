// Package migrations embeds the character service's goose migrations and applies
// them with the service's own version table, goose_character_version, so its schema
// changes on its own (Low-level design, page 10). cmd/migrate applies every
// service's set; the tests of the character module apply this one alone.
package migrations

import (
	"context"
	"embed"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/pkg/dbx"
)

// Table is where goose records this set's versions.
const Table = "goose_character_version"

// FS holds every migration file, NNNNN_name.sql, in order.
//
//go:embed *.sql
var FS embed.FS

// Apply runs every pending migration and reports how many it applied.
func Apply(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	return dbx.Migrate(ctx, pool, FS, Table)
}

// Up is Apply without the count, in the shape pgtest.MigrateFunc wants.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := Apply(ctx, pool)
	return err
}
