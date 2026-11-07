// Package idempotencypg keeps idempotency keys in Postgres, in the caller's transaction.
package idempotencypg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"time"

	"github.com/jackc/pgx/v5"

	"finga/pkg/dbx"
	"finga/pkg/idempotency"
)

// NewStore binds a store to db, usually a transaction, stamping its rows with now.
func NewStore(db dbx.DBTX, now dbx.Stamp) idempotency.Store {
	return &store{db: db, now: now}
}

type store struct {
	db  dbx.DBTX
	now dbx.Stamp
}

const (
	lockSQL   = `SELECT pg_advisory_xact_lock($1)`
	findSQL   = `SELECT request_hash, response FROM idempotency_keys WHERE owner = $1 AND scope = $2 AND key = $3`
	insertSQL = `INSERT INTO idempotency_keys (owner, scope, key, request_hash, response, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $6)`
	pruneSQL  = `DELETE FROM idempotency_keys WHERE (owner, scope, key) IN (
		SELECT owner, scope, key FROM idempotency_keys WHERE created_at < $1 ORDER BY created_at LIMIT $2 FOR UPDATE SKIP LOCKED)`
)

// Prune deletes at most limit keys stored before before, and never one younger than idempotency.MinKeep, oldest first, skipping those another transaction holds; a retry with a pruned key runs its command again.
func Prune(ctx context.Context, db dbx.DBTX, before time.Time, limit int) (int64, error) {
	if floor := time.Now().Add(-idempotency.MinKeep); before.After(floor) {
		before = floor
	}
	tag, err := db.Exec(ctx, pruneSQL, before, limit)
	if err != nil {
		return 0, fmt.Errorf("idempotency: prune: %w", err)
	}
	return tag.RowsAffected(), nil
}

// Do implements idempotency.Store.
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
			return nil, idempotency.ErrKeyReused
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
	if _, err := s.db.Exec(ctx, insertSQL, owner, scope, key, fingerprint, response, s.now); err != nil {
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
