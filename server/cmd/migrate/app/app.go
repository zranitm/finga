// Package app is the migrate binary's composition root: the migration set
// of every service with tables, in the order they are applied, and the
// guard against a database migrated before the split. It is a package of
// its own, next to the main, because the end-to-end tests prepare their
// database exactly the way the binary does.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"

	"finga/pkg/dbx"
	accountmigrations "finga/services/account/migrations"
	charactermigrations "finga/services/character/migrations"
	itemmigrations "finga/services/item/migrations"
)

// LegacyTable is goose's default version table, which only a database
// migrated before the split has: every service's set records its versions
// in a table of its own.
const LegacyTable = "goose_db_version"

// ErrLegacy is a database migrated before the split.
var ErrLegacy = errors.New("migrate: the database was migrated before the services split (it has " + LegacyTable + "); recreate it, for example with make down, then migrate again")

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

// Apply refuses a database migrated before the split, then applies every
// set and reports how many migrations each applied.
func Apply(ctx context.Context, pool *pgxpool.Pool) (map[string]int, error) {
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
	return applied, nil
}

// Up is Apply without the counts, in the shape pgtest.MigrateFunc wants.
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := Apply(ctx, pool)
	return err
}
