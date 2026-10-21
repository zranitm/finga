// Package dbx is the thin layer under every module's store: a connection
// pool from a URL, the DBTX interface that a pool and a transaction both
// satisfy, and InTx, which runs a function inside a transaction and re-runs
// the whole of it on a deadlock or serialization failure. Only module stores
// call InTx; services depend on their module's Store and never see a
// transaction.
package dbx

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is what a repository runs its queries on. *pgxpool.Pool and pgx.Tx
// both satisfy it, as does sqlc's generated DBTX.
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
	retryBaseDelay     = 10 * time.Millisecond
	retryJitter        = 20 * time.Millisecond
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

// OnRetry is called with the error of each attempt that is about to be
// retried, for a retry counter.
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

// InTx runs fn inside a READ COMMITTED transaction and commits it when fn
// returns nil. An error from fn rolls back and is returned as it is, so
// errors.As still finds the *pgconn.PgError underneath; a panic rolls back
// and is re-raised. On a deadlock or serialization failure, from fn or from
// commit, the whole of fn runs again after a short jittered wait, up to 3
// times in all, unless ctx is done first.
//
// Because fn may run more than once, it must have no side effects outside
// the transaction: it sends nothing and changes nothing in memory. Events
// leave through the outbox.
func InTx(ctx context.Context, db Beginner, fn func(tx pgx.Tx) error, opts ...Option) error {
	o := options{maxAttempts: defaultMaxAttempts}
	for _, opt := range opts {
		opt(&o)
	}
	for attempt := 1; ; attempt++ {
		err := runOnce(ctx, db, fn)
		if err == nil || !retryable(err) || attempt >= o.maxAttempts {
			return err
		}
		if o.onRetry != nil {
			o.onRetry(err)
		}
		if waitErr := wait(ctx, backoff(attempt)); waitErr != nil {
			return fmt.Errorf("dbx: retry abandoned: %w (last error: %w)", waitErr, err)
		}
	}
}

func runOnce(ctx context.Context, db Beginner, fn func(tx pgx.Tx) error) (err error) {
	tx, err := db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return fmt.Errorf("dbx: begin: %w", err)
	}
	// Rollback must run even when ctx is already cancelled, or the
	// connection goes back to the pool with an open transaction.
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

func backoff(attempt int) time.Duration {
	return time.Duration(attempt)*retryBaseDelay + rand.N(retryJitter) //nolint:gosec // jitter, not security
}

func wait(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
