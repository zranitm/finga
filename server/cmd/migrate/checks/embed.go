// Package checks embeds migrate's own goose migrations: data checks that run once per database, after every service's set.
package checks

import "embed"

// Table is where goose records which checks a database has passed.
const Table = "goose_migrate_version"

// NotV7 is the SQLSTATE a check raises for IDs that are not UUIDv7s, its message counting them per column.
const NotV7 = "UUID7"

// FS holds every check, NNNNN_name.sql, in order.
//
//go:embed *.sql
var FS embed.FS
