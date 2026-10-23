// Package migrations embeds the goose SQL migrations and applies them. One
// ordered directory, each file touching one module's tables; cmd/migrate
// runs Apply at deploy time and pkg/pgtest runs it on the test template.
package migrations

import (
	"context"
	"embed"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

// FS holds every migration file, NNNNN_name.sql, in order.
//
//go:embed *.sql
var FS embed.FS

// Apply runs every pending migration on pool's database and reports how
// many it applied. A second run applies nothing and returns 0.
func Apply(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, FS)
	if err != nil {
		return 0, fmt.Errorf("migrations: %w", err)
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return 0, fmt.Errorf("migrations: %w", err)
	}
	return len(results), nil
}

// Up is Apply without the count, in the shape pgtest.MigrateFunc wants.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := Apply(ctx, pool)
	return err
}
