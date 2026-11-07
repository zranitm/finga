package retention_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/pkg/retention"
)

// table holds rows by age; it is the Delete, the Observer and the log under test, behind one lock.
type table struct {
	mu     sync.Mutex
	ages   []time.Time
	limits []int
	at     []time.Time   // when each call came
	fail   map[int]error // by call, from 1
	seen   []int64
	logs   bytes.Buffer
}

func (tb *table) delete(_ context.Context, before time.Time, limit int) (int64, error) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.limits = append(tb.limits, limit)
	tb.at = append(tb.at, time.Now())
	if err := tb.fail[len(tb.limits)]; err != nil {
		return 0, err
	}
	var kept []time.Time
	var n int64
	for _, at := range tb.ages {
		if at.Before(before) && n < int64(limit) {
			n++
			continue
		}
		kept = append(kept, at)
	}
	tb.ages = kept
	return n, nil
}

func (tb *table) Deleted(name string, n int64) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	if name == "keys" {
		tb.seen = append(tb.seen, n)
	}
}

func (tb *table) Write(p []byte) (int, error) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return tb.logs.Write(p)
}

// state is the rows left, the calls made, what each pass reported and the log so far.
func (tb *table) state() (left, calls int, seen []int64, logs string) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return len(tb.ages), len(tb.limits), slices.Clone(tb.seen), tb.logs.String()
}

func start(tb *table, p retention.Policy) (stop func() error) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() { done <- retention.Run(ctx, "keys", p, tb.delete, tb, slog.New(slog.NewTextHandler(tb, nil))) }()
	return func() error { cancel(); return <-done }
}

func TestRunDeletesWhatOutlivedItsKeepEveryInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		now := time.Now()
		tb := &table{ages: []time.Time{now.Add(-50 * time.Hour), now.Add(-30 * time.Hour), now.Add(-26 * time.Hour), now.Add(-23 * time.Hour), now}}
		stop := start(tb, retention.Policy{Keep: 24 * time.Hour, Every: time.Hour, Batch: 2})

		time.Sleep(time.Second)
		synctest.Wait()
		left, calls, seen, logs := tb.state()
		if left != 2 || calls != 2 || !slices.Equal(seen, []int64{3}) {
			t.Fatalf("first pass: %d rows left, %d calls, saw %v; want a full batch and a short one deleting 3", left, calls, seen)
		}
		if !strings.Contains(logs, "store=keys deleted=3") {
			t.Fatalf("log %q lacks the pass", logs)
		}

		time.Sleep(time.Hour)
		synctest.Wait()
		if left, calls, seen, _ := tb.state(); left != 2 || calls != 3 || !slices.Equal(seen, []int64{3, 0}) {
			t.Fatalf("an hour on nothing new is old enough: %d left, %d calls, saw %v", left, calls, seen)
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		if left, _, seen, _ := tb.state(); left != 1 || !slices.Equal(seen, []int64{3, 0, 1}) {
			t.Fatalf("two hours on the 23-hour-old row is 25: %d left, saw %v", left, seen)
		}
		if err := stop(); err != nil {
			t.Fatal(err)
		}
		for _, limit := range tb.limits {
			if limit != 2 {
				t.Fatalf("limits %v, want each the batch", tb.limits)
			}
		}
	})
}

// The batches of a pass are spaced by the policy's gap, so other writers get the table between them.
func TestAPassPausesBetweenBatches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		old := time.Now().Add(-48 * time.Hour)
		tb := &table{ages: []time.Time{old, old, old}}
		stop := start(tb, retention.Policy{Keep: 24 * time.Hour, Every: time.Hour, Batch: 1, Gap: 250 * time.Millisecond})
		time.Sleep(time.Minute)
		synctest.Wait()
		if err := stop(); err != nil {
			t.Fatal(err)
		}
		if len(tb.ages) != 0 || len(tb.at) != 4 {
			t.Fatalf("%d rows left after %d calls; want the pass to finish", len(tb.ages), len(tb.at))
		}
		for i := 1; i < len(tb.at); i++ {
			if gap := tb.at[i].Sub(tb.at[i-1]); gap != 250*time.Millisecond {
				t.Fatalf("batches at %v; want the gap between each", tb.at)
			}
		}
	})
}

func TestAFailedPassIsLoggedAndTheNextTriesAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		tb := &table{ages: []time.Time{time.Now().Add(-48 * time.Hour)}, fail: map[int]error{1: errors.New("database down")}}
		stop := start(tb, retention.Policy{Keep: 24 * time.Hour, Every: time.Minute, Batch: 10})
		synctest.Wait()
		if left, _, _, logs := tb.state(); left != 1 || !strings.Contains(logs, "database down") {
			t.Fatalf("%d left, log %q; want the failure logged and the row kept", left, logs)
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if left, _, seen, _ := tb.state(); left != 0 || !slices.Equal(seen, []int64{0, 1}) {
			t.Fatalf("%d left, saw %v; want the next pass to delete it", left, seen)
		}
		if err := stop(); err != nil {
			t.Fatal(err)
		}
	})
}
