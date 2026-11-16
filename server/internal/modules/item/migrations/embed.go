// Package migrations embeds the item module's goose migrations, versioned in their own table.
package migrations

import "embed"

// Table is where goose records this set's versions.
const Table = "goose_item_version"

// FS holds every migration file, NNNNN_name.sql, in order.
//
//go:embed *.sql
var FS embed.FS
