// Package app is the migrate binary's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/cmd/migrate/checks"
	"finga/pkg/dbx"
	accountmigrations "finga/services/account/migrations"
	charactermigrations "finga/services/character/migrations"
	itemmigrations "finga/services/item/migrations"
)

// LegacyTable is goose's default version table, found only in a database migrated before the split.
const LegacyTable = "goose_db_version"

// ErrLegacy is a database migrated before the split.
var ErrLegacy = errors.New("migrate: the database was migrated before the services split (it has " + LegacyTable + "), and no migration upgrades it; copy out what you need and point migrate at an empty database")

// ErrNotV7 is a database holding IDs minted before every ID was a UUIDv7, which no service parses.
var ErrNotV7 = errors.New("migrate: the database predates UUIDv7 IDs, which the services require, and no migration upgrades it; copy out what you need and point migrate at an empty database")

// Set is one service's migrations.
type Set struct {
	Service string
	FS      fs.FS
	Table   string
}

// Sets are every service's migrations, in the order Apply runs them.
func Sets() []Set {
	return []Set{
		{Service: "account", FS: accountmigrations.FS, Table: accountmigrations.Table},
		{Service: "character", FS: charactermigrations.FS, Table: charactermigrations.Table},
		{Service: "item", FS: itemmigrations.FS, Table: itemmigrations.Table},
	}
}

// Apply refuses a pre-split database, applies every set and reports each one's count, then runs the checks a database has not yet passed, so they see what the sets wrote; it holds the migration lock throughout, so two runs at once take turns.
func Apply(ctx context.Context, pool *pgxpool.Pool) (map[string]int, error) {
	var applied map[string]int
	err := dbx.Locked(ctx, pool, func() error {
		var err error
		applied, err = apply(ctx, pool)
		return err
	})
	return applied, err
}

func apply(ctx context.Context, pool *pgxpool.Pool) (map[string]int, error) {
	legacy, err := dbx.TableExists(ctx, pool, LegacyTable)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if legacy {
		return nil, ErrLegacy
	}
	applied := map[string]int{}
	for _, s := range Sets() {
		n, err := dbx.Migrate(ctx, pool, s.FS, s.Table)
		if err != nil {
			return applied, fmt.Errorf("migrate %s: %w", s.Service, err)
		}
		applied[s.Service] = n
	}
	if _, err := dbx.Migrate(ctx, pool, checks.FS, checks.Table); err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == checks.NotV7 {
			return applied, fmt.Errorf("%w (not UUIDv7: %s)", ErrNotV7, pgErr.Message)
		}
		return applied, fmt.Errorf("migrate checks: %w", err)
	}
	return applied, nil
}
