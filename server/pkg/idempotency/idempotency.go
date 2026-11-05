// Package idempotency runs a command at most once per key, inside the caller's transaction.
package idempotency

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"

	"github.com/jackc/pgx/v5"

	"finga/pkg/dbx"
)

// ErrKeyReused is returned when a key is replayed with a different request.
var ErrKeyReused = errors.New("idempotency: key reused with a different request")

// Store runs commands once per (owner, scope, key).
type Store interface {
	// Do runs fn once per key and replays its stored response; fn's error stores nothing. fingerprint
	// identifies the request (see Fingerprint): a key replayed with another is ErrKeyReused.
	Do(ctx context.Context, owner, scope, key string, fingerprint []byte, fn func() ([]byte, error)) ([]byte, error)
}

// NewStore binds a store to db, usually a transaction.
func NewStore(db dbx.DBTX) Store {
	return &store{db: db}
}

type store struct {
	db dbx.DBTX
}

const (
	lockSQL   = `SELECT pg_advisory_xact_lock($1)`
	findSQL   = `SELECT request_hash, response FROM idempotency_keys WHERE owner = $1 AND scope = $2 AND key = $3`
	insertSQL = `INSERT INTO idempotency_keys (owner, scope, key, request_hash, response) VALUES ($1, $2, $3, $4, $5)`
)

// Do implements Store.
func (s *store) Do(ctx context.Context, owner, scope, key string, fingerprint []byte, fn func() ([]byte, error)) ([]byte, error) {
	// Serialise uses of one key: the second waits for the first to commit, then sees its row.
	if _, err := s.db.Exec(ctx, lockSQL, lockID(owner, scope, key)); err != nil {
		return nil, fmt.Errorf("idempotency: lock: %w", err)
	}
	var storedHash, response []byte
	err := s.db.QueryRow(ctx, findSQL, owner, scope, key).Scan(&storedHash, &response)
	switch {
	case err == nil:
		if !bytes.Equal(storedHash, fingerprint) {
			return nil, ErrKeyReused
		}
		return response, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, fmt.Errorf("idempotency: find: %w", err)
	}
	response, err = fn()
	if err != nil {
		return nil, err
	}
	if response == nil {
		response = []byte{}
	}
	if _, err := s.db.Exec(ctx, insertSQL, owner, scope, key, fingerprint, response); err != nil {
		return nil, fmt.Errorf("idempotency: store: %w", err)
	}
	return response, nil
}

// lockID folds the key into the 64-bit space of advisory locks.
func lockID(owner, scope, key string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(owner))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(scope))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(key))
	return int64(h.Sum64()) //nolint:gosec // a hash, wrapping is fine
}
