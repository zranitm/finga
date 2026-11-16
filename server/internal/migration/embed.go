// Package migration embeds the server's goose migrations: one sequence for every module, versioned in one table.
package migration

import "embed"

// Table is where goose records the sequence's versions.
const Table = "goose_db_version"

// FS holds every migration file, NNNNN_module_name.sql, in order.
//
//go:embed *.sql
var FS embed.FS
