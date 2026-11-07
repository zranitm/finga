// Package built holds SQL split across literals and constants, for the stamping guard's Go scan.
package built

const table = "keys"

const markPublished = "UPDATE outbox " +
	"SET published_at = $1 " +
	"WHERE event_id = $2"

var insertKey = "INSERT INTO " + table + " (owner, created_at) VALUES ($1, $2)"

const stamped = "INSERT INTO outbox (id, created_at, updated_at) VALUES ($1, $2, $2)"
