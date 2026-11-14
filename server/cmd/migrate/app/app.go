// Package app is the migrate binary's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"

	accountmigrations "finga/modules/account/migrations"
	charactermigrations "finga/modules/character/migrations"
	itemmigrations "finga/modules/item/migrations"
	"finga/pkg/dbx"
)

// Set is one module's migrations.
type Set struct {
	Service string
	FS      fs.FS
	Table   string
}

// Sets are every module's migrations, in the order Apply runs them.
func Sets() []Set {
	return []Set{
		{Service: "account", FS: accountmigrations.FS, Table: accountmigrations.Table},
		{Service: "character", FS: charactermigrations.FS, Table: charactermigrations.Table},
		{Service: "item", FS: itemmigrations.FS, Table: itemmigrations.Table},
	}
}

// Apply applies every set and reports each one's count; it holds the migration lock, so two runs at once take turns.
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
