// Package migrations embeds the character module's goose migrations.
package migrations

import "embed"

// Table is where goose records this set's versions.
const Table = "goose_character_version"

// FS holds every migration file, NNNNN_name.sql, in order.
//
//go:embed *.sql
var FS embed.FS
