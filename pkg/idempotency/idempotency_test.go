package idempotency_test

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"

	"finga/migrations"
	"finga/pkg/dbx"
	"finga/pkg/idempotency"
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

var errBoom = errors.New("boom")

// run executes one command through a transaction, the way a module's unit
// of work would: the store is bound to the transaction.
func run(ctx context.Context, pool dbx.Beginner, owner, scope, key string, hash []byte, fn func() ([]byte, error)) ([]byte, error) {
	var out []byte
	err := dbx.InTx(ctx, pool, func(tx pgx.Tx) error {
		var err error
		out, err = idempotency.NewStore(tx).Do(ctx, owner, scope, key, hash, fn)
		return err
	})
	return out, err
}

func TestDo(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	effects := 0
	effect := func() ([]byte, error) { effects++; return []byte(`{"ok":true}`), nil }

	t.Run("the first call runs the command and stores its response", func(t *testing.T) {
		out, err := run(ctx, pool, "acc-1", "discard", "k-1", []byte("h1"), effect)
		if err != nil || string(out) != `{"ok":true}` || effects != 1 {
			t.Fatalf("out %s err %v effects %d", out, err, effects)
		}
	})
	t.Run("the same key with the same request returns the stored response and runs nothing", func(t *testing.T) {
		out, err := run(ctx, pool, "acc-1", "discard", "k-1", []byte("h1"), func() ([]byte, error) { effects++; return []byte("other"), nil })
		if err != nil || string(out) != `{"ok":true}` || effects != 1 {
			t.Fatalf("out %s err %v effects %d", out, err, effects)
		}
	})
	t.Run("the same key with a different request is refused", func(t *testing.T) {
		_, err := run(ctx, pool, "acc-1", "discard", "k-1", []byte("h2"), effect)
		if !errors.Is(err, idempotency.ErrKeyReused) || effects != 1 {
			t.Fatalf("err %v effects %d", err, effects)
		}
	})
	t.Run("keys are scoped by owner and by scope", func(t *testing.T) {
		before := effects
		if _, err := run(ctx, pool, "acc-2", "discard", "k-1", []byte("h1"), effect); err != nil {
			t.Fatal(err)
		}
		if _, err := run(ctx, pool, "acc-1", "move", "k-1", []byte("h1"), effect); err != nil {
			t.Fatal(err)
		}
		if effects != before+2 {
			t.Fatalf("effects %d, want %d", effects, before+2)
		}
	})
	t.Run("a failing command stores nothing, so a retry runs it", func(t *testing.T) {
		before := effects
		_, err := run(ctx, pool, "acc-1", "discard", "k-fail", []byte("h"), func() ([]byte, error) { return nil, errBoom })
		if !errors.Is(err, errBoom) {
			t.Fatalf("err %v", err)
		}
		out, err := run(ctx, pool, "acc-1", "discard", "k-fail", []byte("h"), effect)
		if err != nil || string(out) != `{"ok":true}` || effects != before+1 {
			t.Fatalf("retry: out %s err %v effects %d", out, err, effects)
		}
	})
	t.Run("a nil response is stored as empty and replayed as empty", func(t *testing.T) {
		if out, err := run(ctx, pool, "acc-1", "discard", "k-nil", []byte("h"), func() ([]byte, error) { return nil, nil }); err != nil || len(out) != 0 {
			t.Fatalf("out %q err %v", out, err)
		}
		if out, err := run(ctx, pool, "acc-1", "discard", "k-nil", []byte("h"), effect); err != nil || len(out) != 0 {
			t.Fatalf("replay: out %q err %v", out, err)
		}
	})
}

func TestConcurrentSameKeyRunsOnce(t *testing.T) {
	ctx := context.Background()
	pool := cluster.DB(t)
	var effects atomic.Int32
	var wg sync.WaitGroup
	outs := make([]string, 8)
	errs := make([]error, 8)
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := run(ctx, pool, "acc-9", "pickup", "drop-1", []byte("h"), func() ([]byte, error) {
				effects.Add(1)
				return []byte("got it"), nil
			})
			outs[i], errs[i] = string(out), err
		}()
	}
	wg.Wait()
	if effects.Load() != 1 {
		t.Fatalf("the command ran %d times, want 1", effects.Load())
	}
	for i := range 8 {
		if errs[i] != nil || outs[i] != "got it" {
			t.Fatalf("caller %d: %q %v; every caller should get the one response", i, outs[i], errs[i])
		}
	}
}
