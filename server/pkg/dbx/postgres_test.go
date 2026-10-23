package dbx_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"finga/migrations"
	"finga/pkg/dbx"
	"finga/pkg/pgtest"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, migrations.Up)
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
		// Two transactions lock the same two rows in opposite orders; Postgres
		// kills one with 40P01 after deadlock_timeout, InTx re-runs it, and both
		// updates land. Each function counts how often it ran.
		var runs [2]int
		var retries [2]int
		var mu sync.Mutex
		started := make(chan struct{}, 2)
		proceed := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)
		order := [2][2]int{{10, 11}, {11, 10}}
		for i := range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
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
				}, dbx.OnRetry(func(error) { mu.Lock(); retries[i]++; mu.Unlock() }))
			}()
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
		if retries[0]+retries[1] != 1 {
			t.Fatalf("retries = %v, want exactly one", retries)
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

func TestNewPool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	t.Run("connects and pings", func(t *testing.T) {
		pool, err := dbx.NewPool(ctx, cluster.URL(t), 3)
		if err != nil {
			t.Fatal(err)
		}
		defer pool.Close()
		if got := pool.Config().MaxConns; got != 3 {
			t.Fatalf("max_conns = %d, want 3", got)
		}
	})
	t.Run("a bad URL is rejected", func(t *testing.T) {
		if _, err := dbx.NewPool(ctx, "postgres://finga:wrong@127.0.0.1:1/nope?sslmode=disable&connect_timeout=1", 1); err == nil {
			t.Fatal("no error")
		}
	})
	t.Run("malformed URL is rejected", func(t *testing.T) {
		if _, err := dbx.NewPool(ctx, "::not a url", 1); err == nil {
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
