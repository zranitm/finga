package dbx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"finga/pkg/dbx"
)

// fakeTx is a pgx.Tx that only knows how to commit and roll back; every
// other method is the embedded nil interface and would panic if called.
type fakeTx struct {
	pgx.Tx
	commitErr  error
	committed  bool
	rolledBack bool
}

func (t *fakeTx) Commit(context.Context) error   { t.committed = true; return t.commitErr }
func (t *fakeTx) Rollback(context.Context) error { t.rolledBack = true; return nil }

// fakeBeginner hands out one fakeTx per Begin, so a test can see what
// happened to each attempt.
type fakeBeginner struct {
	beginErr  error
	commitErr []error // per attempt; nil beyond the slice
	txs       []*fakeTx
	isoLevels []pgx.TxIsoLevel
}

func (b *fakeBeginner) BeginTx(_ context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	if b.beginErr != nil {
		return nil, b.beginErr
	}
	tx := &fakeTx{}
	if n := len(b.txs); n < len(b.commitErr) {
		tx.commitErr = b.commitErr[n]
	}
	b.txs = append(b.txs, tx)
	b.isoLevels = append(b.isoLevels, opts.IsoLevel)
	return tx, nil
}

func pgErr(code string) *pgconn.PgError { return &pgconn.PgError{Code: code, Message: "fake"} }

var errBoom = errors.New("boom")

func TestInTxOutcome(t *testing.T) {
	tests := []struct {
		name           string
		fn             func(pgx.Tx) error
		commitErr      error
		wantErr        error // errors.Is target, nil for success
		wantCommitted  bool
		wantRolledBack bool
	}{
		{"commits on success", func(pgx.Tx) error { return nil }, nil, nil, true, false},
		{"rolls back on an error from fn", func(pgx.Tx) error { return errBoom }, nil, errBoom, false, true},
		{"a failing commit is reported", func(pgx.Tx) error { return nil }, errBoom, errBoom, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &fakeBeginner{commitErr: []error{tt.commitErr}}
			err := dbx.InTx(context.Background(), db, tt.fn)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if len(db.txs) != 1 {
				t.Fatalf("%d transactions begun, want 1", len(db.txs))
			}
			tx := db.txs[0]
			if tx.committed != tt.wantCommitted || tx.rolledBack != tt.wantRolledBack {
				t.Fatalf("committed=%v rolledBack=%v, want %v %v", tx.committed, tx.rolledBack, tt.wantCommitted, tt.wantRolledBack)
			}
			if db.isoLevels[0] != pgx.ReadCommitted {
				t.Fatalf("isolation %q, want read committed", db.isoLevels[0])
			}
		})
	}
}

func TestInTxBeginError(t *testing.T) {
	db := &fakeBeginner{beginErr: errBoom}
	if err := dbx.InTx(context.Background(), db, func(pgx.Tx) error { return nil }); !errors.Is(err, errBoom) {
		t.Fatalf("err = %v", err)
	}
}

func TestInTxPanicRollsBackAndRePanics(t *testing.T) {
	db := &fakeBeginner{}
	defer func() {
		p := recover()
		if p != "kaboom" {
			t.Fatalf("recovered %v, want the original panic", p)
		}
		if len(db.txs) != 1 || !db.txs[0].rolledBack || db.txs[0].committed {
			t.Fatalf("txs = %+v, want one rolled back", db.txs)
		}
	}()
	_ = dbx.InTx(context.Background(), db, func(pgx.Tx) error { panic("kaboom") })
}

func TestInTxErrorsStayReachable(t *testing.T) {
	tests := []struct {
		name string
		fn   func(pgx.Tx) error
		db   *fakeBeginner
	}{
		{"PgError from fn", func(pgx.Tx) error { return pgErr("23505") }, &fakeBeginner{}},
		{"PgError from commit", func(pgx.Tx) error { return nil }, &fakeBeginner{commitErr: []error{pgErr("23505")}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dbx.InTx(context.Background(), tt.db, tt.fn)
			var pe *pgconn.PgError
			if !errors.As(err, &pe) || pe.Code != "23505" {
				t.Fatalf("errors.As found no PgError 23505 in %v", err)
			}
		})
	}
}

func TestInTxRetries(t *testing.T) {
	tests := []struct {
		name         string
		fnErrs       []error // returned by fn on attempts 1, 2, 3...; nil beyond the slice means success
		commitErrs   []error
		wantAttempts int
		wantCode     string // PgError code on the final error, empty for success
		wantRetries  int
	}{
		{"serialization failure once, then success", []error{pgErr("40001")}, nil, 2, "", 1},
		{"deadlock twice, then success", []error{pgErr("40P01"), pgErr("40P01")}, nil, 3, "", 2},
		{"three failures exhaust the limit", []error{pgErr("40001"), pgErr("40001"), pgErr("40001")}, nil, 3, "40001", 2},
		{"a retryable commit error is retried", nil, []error{pgErr("40001")}, 2, "", 1},
		{"a unique violation is not retried", []error{pgErr("23505")}, nil, 1, "23505", 0},
		{"a plain error is not retried", []error{errBoom}, nil, 1, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &fakeBeginner{commitErr: tt.commitErrs}
			attempts, retries := 0, 0
			err := dbx.InTx(context.Background(), db, func(pgx.Tx) error {
				attempts++
				if attempts-1 < len(tt.fnErrs) {
					return tt.fnErrs[attempts-1]
				}
				return nil
			}, dbx.OnRetry(func(error) { retries++ }))
			if attempts != tt.wantAttempts || len(db.txs) != tt.wantAttempts {
				t.Fatalf("attempts = %d (txs %d), want %d", attempts, len(db.txs), tt.wantAttempts)
			}
			if retries != tt.wantRetries {
				t.Fatalf("retries = %d, want %d", retries, tt.wantRetries)
			}
			var pe *pgconn.PgError
			switch {
			case tt.wantCode != "":
				if !errors.As(err, &pe) || pe.Code != tt.wantCode {
					t.Fatalf("err = %v, want PgError %s", err, tt.wantCode)
				}
			case tt.fnErrs != nil && errors.Is(tt.fnErrs[0], errBoom):
				if !errors.Is(err, errBoom) {
					t.Fatalf("err = %v, want boom", err)
				}
			default:
				if err != nil {
					t.Fatalf("err = %v, want success", err)
				}
			}
			// Every attempt but a successful last one must have rolled back.
			for i, tx := range db.txs {
				last := i == len(db.txs)-1
				if !last && !tx.rolledBack && tx.commitErr == nil {
					t.Fatalf("attempt %d neither rolled back nor failed at commit", i+1)
				}
			}
		})
	}
}

func TestInTxStopsWhenTheContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	db := &fakeBeginner{}
	attempts := 0
	err := dbx.InTx(ctx, db, func(pgx.Tx) error {
		attempts++
		cancel() // cancelled while the first attempt is running
		return pgErr("40001")
	})
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1: no retry after cancellation", attempts)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		t.Fatalf("the last database error is not reachable: %v", err)
	}
}
