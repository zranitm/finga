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

// ErrNotV7 is a database holding an ID minted before every ID was a UUIDv7, which no service parses.
var ErrNotV7 = errors.New("migrate: a row holds an ID that is not a UUIDv7, which the services refuse; fix or delete that row, then migrate again")

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

// Apply refuses a pre-split database, runs the checks a database has not yet passed, then applies every set and reports each one's count.
func Apply(ctx context.Context, pool *pgxpool.Pool) (map[string]int, error) {
	legacy, err := dbx.TableExists(ctx, pool, LegacyTable)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if legacy {
		return nil, ErrLegacy
	}
	if _, err := dbx.Migrate(ctx, pool, checks.FS, checks.Table); err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == checks.NotV7 {
			return nil, fmt.Errorf("%w (%s)", ErrNotV7, pgErr.Message)
		}
		return nil, fmt.Errorf("migrate checks: %w", err)
	}
	applied := map[string]int{}
	for _, s := range Sets() {
		n, err := dbx.Migrate(ctx, pool, s.FS, s.Table)
		if err != nil {
			return applied, fmt.Errorf("migrate %s: %w", s.Service, err)
		}
		applied[s.Service] = n
	}
	return applied, nil
}
