// Package dbx is the thin layer under module stores: a pool, the DBTX interface and InTx.
package dbx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is what queries run on; *pgxpool.Pool, pgx.Tx and sqlc's DBTX satisfy it.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Beginner opens transactions; *pgxpool.Pool satisfies it.
type Beginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// Postgres error codes that mean "try the whole transaction again".
const (
	codeSerializationFailure = "40001"
	codeDeadlockDetected     = "40P01"
)

const (
	defaultMaxAttempts = 3
	retryFirst         = 20 * time.Millisecond
	retryMax           = 200 * time.Millisecond
)

// Option tunes InTx.
type Option func(*options)

type options struct {
	maxAttempts int
	onRetry     func(error)
}

// MaxAttempts caps how often InTx runs fn, 3 by default.
func MaxAttempts(n int) Option {
	return func(o *options) {
		if n >= 1 {
			o.maxAttempts = n
		}
	}
}

// OnRetry is called with each error about to be retried.
func OnRetry(fn func(error)) Option {
	return func(o *options) { o.onRetry = fn }
}

// NewPool connects to url with at most maxConns connections and pings it.
func NewPool(ctx context.Context, url string, maxConns int32) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("dbx: parse database url: %w", err)
	}
	cfg.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("dbx: connect: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("dbx: ping: %w", err)
	}
	return pool, nil
}

// InTx runs fn in a READ COMMITTED transaction and reruns it on deadlock or serialization failure,
// so fn must have no side effects outside the transaction.
func InTx(ctx context.Context, db Beginner, fn func(tx pgx.Tx) error, opts ...Option) error {
	o := options{maxAttempts: defaultMaxAttempts}
	for _, opt := range opts {
		opt(&o)
	}
	var last error
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		last = runOnce(ctx, db, fn)
		if last != nil && !retryable(last) {
			return struct{}{}, backoff.Permanent(last)
		}
		return struct{}{}, last
	},
		backoff.WithBackOff(&backoff.ExponentialBackOff{InitialInterval: retryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: retryMax}),
		backoff.WithMaxTries(uint(o.maxAttempts)), //nolint:gosec // MaxAttempts keeps it at least 1
		backoff.WithMaxElapsedTime(0),
		backoff.WithNotify(func(err error, _ time.Duration) {
			if o.onRetry != nil {
				o.onRetry(err)
			}
		}),
	)
	if err != nil && last != nil && !errors.Is(err, last) {
		return fmt.Errorf("dbx: retry abandoned: %w (last error: %w)", err, last)
	}
	return err
}

func runOnce(ctx context.Context, db Beginner, fn func(tx pgx.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("dbx: begin: %w", err)
	}
	// Rollback must run even when ctx is cancelled, or the connection returns to the pool mid-transaction.
	rollbackCtx := context.WithoutCancel(ctx)
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(rollbackCtx)
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback(rollbackCtx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("dbx: commit: %w", err)
	}
	return nil
}

func retryable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == codeSerializationFailure || pgErr.Code == codeDeadlockDetected
}
