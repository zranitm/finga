// Package dbx is the thin layer under module stores: a pool, the DBTX interface and InTx.
package dbx

import (
	"context"
	"errors"
	"fmt"
	"strconv"
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
	maxAttempts = 3
	retryFirst  = 20 * time.Millisecond
	retryMax    = 200 * time.Millisecond
)

// NewPool connects with c's limits on every connection and pings the database.
func NewPool(ctx context.Context, c Config) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(c.URL)
	if err != nil {
		return nil, fmt.Errorf("dbx: parse database url: %w", err)
	}
	cfg.MaxConns = c.MaxConns
	for name, d := range map[string]time.Duration{
		"statement_timeout":                   c.StatementTimeout,
		"lock_timeout":                        c.LockTimeout,
		"idle_in_transaction_session_timeout": c.IdleInTransactionTimeout,
	} {
		if d > 0 {
			cfg.ConnConfig.RuntimeParams[name] = strconv.FormatInt(d.Milliseconds(), 10)
		}
	}
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

// Observer hears how each transaction went: its time, retries included, and how many times it ran.
type Observer interface {
	TxDone(ctx context.Context, took time.Duration, runs int)
}

// TxOption tunes InTx.
type TxOption func(*txOptions)

type txOptions struct{ obs Observer }

// Observed tells obs about the transaction once it is done, whatever its outcome.
func Observed(obs Observer) TxOption { return func(o *txOptions) { o.obs = obs } }

// InTx runs fn in a READ COMMITTED transaction and reruns it on deadlock or serialization failure,
// so fn must have no side effects outside the transaction.
func InTx(ctx context.Context, db Beginner, fn func(tx pgx.Tx) error, opts ...TxOption) error {
	var o txOptions
	for _, opt := range opts {
		opt(&o)
	}
	start, runs := time.Now(), 0
	if o.obs != nil {
		defer func() { o.obs.TxDone(ctx, time.Since(start), runs) }()
	}
	var last error
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		runs++
		last = runOnce(ctx, db, fn)
		if last != nil && !retryable(last) {
			return struct{}{}, backoff.Permanent(last)
		}
		return struct{}{}, last
	},
		backoff.WithBackOff(&backoff.ExponentialBackOff{InitialInterval: retryFirst, RandomizationFactor: backoff.DefaultRandomizationFactor, Multiplier: 2, MaxInterval: retryMax}),
		backoff.WithMaxTries(maxAttempts),
		backoff.WithMaxElapsedTime(0),
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
