package dbx_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"finga/pkg/dbx"
)

// fakeTx is a pgx.Tx that only commits and rolls back; any other method panics.
type fakeTx struct {
	pgx.Tx
	commitErr  error
	committed  bool
	rolledBack bool
}

func (t *fakeTx) Commit(context.Context) error   { t.committed = true; return t.commitErr }
func (t *fakeTx) Rollback(context.Context) error { t.rolledBack = true; return nil }

// fakeBeginner hands out one fakeTx per attempt.
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

// txObserver records how many times each observed transaction ran.
type txObserver struct{ runs []int }

func (o *txObserver) TxDone(_ context.Context, _ time.Duration, runs int) {
	o.runs = append(o.runs, runs)
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
	}{
		{"serialization failure once, then success", []error{pgErr("40001")}, nil, 2, ""},
		{"deadlock twice, then success", []error{pgErr("40P01"), pgErr("40P01")}, nil, 3, ""},
		{"a lock timeout once, then success", []error{pgErr("55P03")}, nil, 2, ""},
		{"a statement timeout is not retried", []error{pgErr("57014")}, nil, 1, "57014"},
		{"three failures exhaust the limit", []error{pgErr("40001"), pgErr("40001"), pgErr("40001")}, nil, 3, "40001"},
		{"a retryable commit error is retried", nil, []error{pgErr("40001")}, 2, ""},
		{"a unique violation is not retried", []error{pgErr("23505")}, nil, 1, "23505"},
		{"a plain error is not retried", []error{errBoom}, nil, 1, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &fakeBeginner{commitErr: tt.commitErrs}
			attempts := 0
			obs := &txObserver{}
			err := dbx.InTx(context.Background(), db, func(pgx.Tx) error {
				attempts++
				if attempts-1 < len(tt.fnErrs) {
					return tt.fnErrs[attempts-1]
				}
				return nil
			}, dbx.Observed(obs))
			if attempts != tt.wantAttempts || len(db.txs) != tt.wantAttempts {
				t.Fatalf("attempts = %d (txs %d), want %d", attempts, len(db.txs), tt.wantAttempts)
			}
			if len(obs.runs) != 1 || obs.runs[0] != tt.wantAttempts {
				t.Fatalf("observed %v, want one transaction that ran %d times", obs.runs, tt.wantAttempts)
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

func TestTimedOut(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"a lock wait past lock_timeout", pgErr("55P03"), true},
		{"a statement past statement_timeout", pgErr("57014"), true},
		{"one wrapped", fmt.Errorf("idempotency: lock: %w", pgErr("55P03")), true},
		{"a deadlock", pgErr("40P01"), false},
		{"a unique violation", pgErr("23505"), false},
		{"a plain error", errBoom, false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dbx.TimedOut(tt.err); got != tt.want {
				t.Fatalf("TimedOut(%v) = %v, want %v", tt.err, got, tt.want)
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
	if _, ok := errors.AsType[*pgconn.PgError](err); !ok {
		t.Fatalf("the last database error is not reachable: %v", err)
	}
}

func TestErrorsClassify(t *testing.T) {
	notFound, timedOut, unavailable, taken := errors.New("not found"), errors.New("timed out"), errors.New("unavailable"), errors.New("name taken")
	classes := dbx.Errors{NotFound: notFound, TimedOut: timedOut, Unavailable: unavailable, Constraints: map[string]error{"characters_name_key": taken}}
	other := &pgconn.PgError{Code: "23505", ConstraintName: "another_key"}
	tests := []struct {
		name string
		err  error
		want error // nil: err passes through as it is
	}{
		{"no row", fmt.Errorf("find: %w", pgx.ErrNoRows), notFound},
		{"a lock wait past its timeout", &pgconn.PgError{Code: "55P03"}, timedOut},
		{"a statement past its timeout", &pgconn.PgError{Code: "57014"}, timedOut},
		{"a constraint the repository names", &pgconn.PgError{Code: "23505", ConstraintName: "characters_name_key"}, taken},
		{"a constraint it does not", other, nil},
		{"a serialization failure InTx's retries did not clear", &pgconn.PgError{Code: "40001"}, timedOut},
		{"a deadlock InTx's retries did not clear", &pgconn.PgError{Code: "40P01"}, timedOut},
		{"a connection lost", &pgconn.PgError{Code: "08006"}, unavailable},
		{"the server shutting down", &pgconn.PgError{Code: "57P01"}, unavailable},
		{"the server starting up", &pgconn.PgError{Code: "57P03"}, unavailable},
		{"too many connections", &pgconn.PgError{Code: "53300"}, unavailable},
		{"a connection cut mid-reply", fmt.Errorf("query: %w", io.ErrUnexpectedEOF), unavailable},
		{"a caller that gave up", fmt.Errorf("query: %w", context.Canceled), nil},
		{"a bug", &pgconn.PgError{Code: "42601"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classes.Classify(tt.err)
			if tt.want == nil {
				if got != tt.err { //nolint:errorlint // passed through means the very error, unwrapped
					t.Fatalf("Classify = %v, want %v as it was", got, tt.err)
				}
				return
			}
			if !errors.Is(got, tt.want) || !errors.Is(got, tt.err) {
				t.Fatalf("Classify = %v, want %v wrapping the cause", got, tt.want)
			}
		})
	}
	if classes.Classify(nil) != nil {
		t.Fatal("nil is classified")
	}
}

func TestRowsOrNotFound(t *testing.T) {
	failed := errors.New("boom")
	if err := dbx.RowsOrNotFound(1, nil); err != nil {
		t.Fatalf("a changed row: %v", err)
	}
	if err := dbx.RowsOrNotFound(0, nil); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("no changed row: %v", err)
	}
	if err := dbx.RowsOrNotFound(0, failed); !errors.Is(err, failed) {
		t.Fatalf("a failed statement: %v", err)
	}
}
