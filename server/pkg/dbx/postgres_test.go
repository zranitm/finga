package dbx_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"finga/pkg/dbx"
	"finga/pkg/pgtest"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, nil) // each test creates the tables it needs
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

func TestInTxAgainstPostgres(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	if _, err := pool.Exec(ctx, `CREATE TABLE t (id int PRIMARY KEY, n int NOT NULL)`); err != nil {
		t.Fatal(err)
	}

	t.Run("a committed insert is visible", func(t *testing.T) {
		err := dbx.InTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `INSERT INTO t VALUES (1, 0)`)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if n := count(t, pool, 1); n != 1 {
			t.Fatalf("rows with id 1 = %d, want 1", n)
		}
	})

	t.Run("a rolled-back insert is not", func(t *testing.T) {
		err := dbx.InTx(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `INSERT INTO t VALUES (2, 0)`); err != nil {
				return err
			}
			return errBoom
		})
		if !errors.Is(err, errBoom) {
			t.Fatalf("err = %v", err)
		}
		if n := count(t, pool, 2); n != 0 {
			t.Fatalf("rows with id 2 = %d, want 0", n)
		}
	})

	t.Run("a deadlocked transaction is re-run as a whole", func(t *testing.T) {
		if _, err := pool.Exec(ctx, `INSERT INTO t VALUES (10, 0), (11, 0)`); err != nil {
			t.Fatal(err)
		}
		// Opposite lock orders make Postgres kill one with 40P01; InTx reruns it and both updates land.
		var runs [2]int
		var mu sync.Mutex
		started := make(chan struct{}, 2)
		proceed := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		order := [2][2]int{{10, 11}, {11, 10}}
		for i := range 2 {
			wg.Go(func() {
				errs[i] = dbx.InTx(ctx, pool, func(tx pgx.Tx) error {
					mu.Lock()
					runs[i]++
					first := runs[i] == 1
					mu.Unlock()
					if _, err := tx.Exec(ctx, `UPDATE t SET n = n + 1 WHERE id = $1`, order[i][0]); err != nil {
						return err
					}
					if first {
						started <- struct{}{}
						<-proceed // both hold their first row before taking the second
					}
					_, err := tx.Exec(ctx, `UPDATE t SET n = n + 1 WHERE id = $1`, order[i][1])
					return err
				})
			})
		}
		<-started
		<-started
		close(proceed)
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("transaction %d: %v", i, err)
			}
		}
		if runs[0]+runs[1] != 3 {
			t.Fatalf("runs = %v, want 3 in total (one re-run)", runs)
		}
		for _, id := range []int{10, 11} {
			var n int
			if err := pool.QueryRow(ctx, `SELECT n FROM t WHERE id = $1`, id).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 2 {
				t.Fatalf("row %d: n = %d, want 2 (both updates applied once)", id, n)
			}
		}
	})
}

// A lock held past lock_timeout is contention, not a fault: the transaction runs again and gets it.
func TestInTxRerunsALockTimeout(t *testing.T) {
	ctx := context.Background()
	pool, err := dbx.NewPool(ctx, dbx.Config{URL: cluster.URL(t), MaxConns: 2, LockTimeout: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `CREATE TABLE t (id int PRIMARY KEY, n int NOT NULL); INSERT INTO t VALUES (1, 0)`); err != nil {
		t.Fatal(err)
	}
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `SELECT * FROM t WHERE id = 1 FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	var codes []string
	err = dbx.InTx(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE t SET n = n + 1 WHERE id = 1`)
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
			codes = append(codes, pgErr.Code)
			_ = holder.Rollback(ctx) // the holder lets go once the first run has timed out
		}
		return err
	})
	if err != nil || len(codes) != 1 || codes[0] != "55P03" {
		t.Fatalf("err %v after failed runs %v, want one lock timeout, then success", err, codes)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT n FROM t WHERE id = 1`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("n = %d (%v), want the update applied once", n, err)
	}
}

// A database out of reach, refusing connections or dropping them, is unavailable, so a caller tries again later.
func TestAnUnreachableDatabaseIsUnavailable(t *testing.T) {
	ctx := context.Background()
	unavailable := errors.New("unavailable")
	classes := dbx.Errors{Unavailable: unavailable}
	t.Run("a refused connection", func(t *testing.T) {
		pool, err := pgxpool.New(ctx, "postgres://finga@127.0.0.1:1/finga?connect_timeout=2")
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		err = dbx.InTx(ctx, pool, func(pgx.Tx) error { return nil })
		if !errors.Is(classes.Classify(err), unavailable) {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("a connection the server ends mid-transaction", func(t *testing.T) {
		err := dbx.InTx(ctx, cluster.DB(t), func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `SELECT pg_terminate_backend(pg_backend_pid())`)
			return err
		})
		if !errors.Is(classes.Classify(err), unavailable) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestNewPool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	t.Run("connects and pings", func(t *testing.T) {
		pool, err := dbx.NewPool(ctx, dbx.Config{URL: cluster.URL(t), MaxConns: 3})
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		if got := pool.Config().MaxConns; got != 3 {
			t.Fatalf("max_conns = %d, want 3", got)
		}
	})
	// A stuck lock or a forgotten transaction must not park a connection, and the workers on it, forever.
	t.Run("every connection has the timeouts", func(t *testing.T) {
		pool, err := dbx.NewPool(ctx, dbx.Config{URL: cluster.URL(t), MaxConns: 2, StatementTimeout: 300 * time.Millisecond, LockTimeout: 200 * time.Millisecond, IdleInTransactionTimeout: 2 * time.Second})
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		for setting, want := range map[string]string{"statement_timeout": "300ms", "lock_timeout": "200ms", "idle_in_transaction_session_timeout": "2s"} {
			var got string
			if err := pool.QueryRow(ctx, "SHOW "+setting).Scan(&got); err != nil || got != want {
				t.Errorf("%s = %q (%v), want %s", setting, got, err, want)
			}
		}
		var pgErr *pgconn.PgError
		if _, err := pool.Exec(ctx, "SELECT pg_sleep(2)"); !errors.As(err, &pgErr) || pgErr.Code != "57014" {
			t.Fatalf("a 2 s statement: %v, want query_canceled", err)
		}
	})
	t.Run("zero leaves the server's default", func(t *testing.T) {
		pool, err := dbx.NewPool(ctx, dbx.Config{URL: cluster.URL(t), MaxConns: 1})
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		var got string
		if err := pool.QueryRow(ctx, "SHOW statement_timeout").Scan(&got); err != nil || got != "0" {
			t.Fatalf("statement_timeout = %q (%v)", got, err)
		}
	})
	t.Run("a bad URL is rejected", func(t *testing.T) {
		if _, err := dbx.NewPool(ctx, dbx.Config{URL: "postgres://finga:wrong@127.0.0.1:1/nope?sslmode=disable&connect_timeout=1", MaxConns: 1}); err == nil {
			t.Fatal("no error")
		}
	})
	t.Run("malformed URL is rejected", func(t *testing.T) {
		if _, err := dbx.NewPool(ctx, dbx.Config{URL: "::not a url", MaxConns: 1}); err == nil {
			t.Fatal("no error")
		}
	})
}

func count(t *testing.T, db dbx.DBTX, id int) int {
	t.Helper()
	var n int
	if err := db.QueryRow(context.Background(), `SELECT count(*) FROM t WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
