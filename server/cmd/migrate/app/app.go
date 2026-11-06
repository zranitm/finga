// Package app is the migrate binary's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/pkg/dbx"
	accountmigrations "finga/services/account/migrations"
	charactermigrations "finga/services/character/migrations"
	itemmigrations "finga/services/item/migrations"
)

// LegacyTable is goose's default version table, found only in a database migrated before the split.
const LegacyTable = "goose_db_version"

// ErrLegacy is a database migrated before the split.
var ErrLegacy = errors.New("migrate: the database was migrated before the services split (it has " + LegacyTable + "); recreate it, for example with make down, then migrate again")

// ErrNotV7 is a database holding an ID minted before every ID was a UUIDv7, which no service parses.
var ErrNotV7 = errors.New("migrate: the database holds IDs minted before every ID was a UUIDv7, which the services refuse; recreate it, for example with make down, then migrate again")

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

// Apply refuses a pre-split database or one holding older IDs, then applies every set and reports each one's count.
func Apply(ctx context.Context, pool *pgxpool.Pool) (map[string]int, error) {
	legacy, err := dbx.TableExists(ctx, pool, LegacyTable)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if legacy {
		return nil, ErrLegacy
	}
	older, err := notV7(ctx, pool)
	if err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if older != "" {
		return nil, fmt.Errorf("%w (%s)", ErrNotV7, older)
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

// uuidColumnsSQL lists every uuid column of the schema's tables: every service's IDs.
const uuidColumnsSQL = `
SELECT c.relname, a.attname
FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid
WHERE c.relnamespace = to_regnamespace(current_schema()) AND c.relkind = 'r'
  AND a.atttypid = 'uuid'::regtype AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY c.relname, a.attnum`

// notV7 names a column holding an ID that is not a UUIDv7, with that ID; it is empty when every ID is one.
func notV7(ctx context.Context, db dbx.DBTX) (string, error) {
	rows, err := db.Query(ctx, uuidColumnsSQL)
	if err != nil {
		return "", err
	}
	columns, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([2]string, error) {
		var c [2]string
		return c, row.Scan(&c[0], &c[1])
	})
	if err != nil {
		return "", err
	}
	for _, c := range columns {
		table, column := pgx.Identifier{c[0]}.Sanitize(), pgx.Identifier{c[1]}.Sanitize()
		var id string
		err := db.QueryRow(ctx, `SELECT `+column+`::text FROM `+table+` WHERE `+column+` IS NOT NULL AND uuid_extract_version(`+column+`) IS DISTINCT FROM 7 LIMIT 1`).Scan(&id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
		case err != nil:
			return "", err
		default:
			return c[0] + "." + c[1] + " holds " + id, nil
		}
	}
	return "", nil
}
