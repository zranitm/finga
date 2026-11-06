// Package idempotency runs a command at most once per key, inside the caller's transaction; idempotencypg stores the keys in Postgres.
package idempotency

import (
	"context"
	"errors"
)

// ErrKeyReused is returned when a key is replayed with a different request.
var ErrKeyReused = errors.New("idempotency: key reused with a different request")

// Store runs commands once per (owner, scope, key).
type Store interface {
	// Do runs fn once per key and replays its response; fn's error stores nothing, another fingerprint is ErrKeyReused.
	Do(ctx context.Context, owner, scope, key string, fingerprint []byte, fn func() ([]byte, error)) ([]byte, error)
}
