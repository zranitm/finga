// Package checks embeds migrate's own goose migrations: data checks that run once per database, before any service's set.
package checks

import "embed"

// Table is where goose records which checks a database has passed.
const Table = "goose_migrate_version"

// NotV7 is the SQLSTATE a check raises for an ID that is not a UUIDv7.
const NotV7 = "UUID7"

// FS holds every check, NNNNN_name.sql, in order.
//
//go:embed *.sql
var FS embed.FS
