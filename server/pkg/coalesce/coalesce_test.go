package coalesce_test

import (
	"sync"
	"testing"
	"testing/synctest"

	"finga/pkg/coalesce"
)

type save struct {
	key string
	n   int
}

func newQueue() *coalesce.Queue[string, save] {
	return coalesce.New(func(s save) string { return s.key })
}

func take(t *testing.T, q *coalesce.Queue[string, save], want save) {
	t.Helper()
	if got, ok := q.Take(); !ok || got != want {
		t.Fatalf("Take = %v, %t; want %v", got, ok, want)
	}
}

func empty(t *testing.T, q *coalesce.Queue[string, save]) {
	t.Helper()
	if got, ok := q.Take(); ok {
		t.Fatalf("Take = %v; want nothing", got)
	}
}

func TestANewerValueReplacesOneWaitingAndKeepsItsPlace(t *testing.T) {
	q := newQueue()
	q.Put(save{"a", 1})
	q.Put(save{"b", 1})
	q.Put(save{"a", 2})
	take(t, q, save{"a", 2})
	take(t, q, save{"b", 1})
	empty(t, q)
}

func TestAKeyTakenWaitsForDoneAndThenJoinsTheEnd(t *testing.T) {
	q := newQueue()
	q.Put(save{"a", 1})
	a := save{"a", 1}
	take(t, q, a)
	q.Put(save{"a", 2})
	q.Put(save{"b", 1})
	take(t, q, save{"b", 1})
	empty(t, q)
	q.Done(a, false)
	take(t, q, save{"a", 2})
}

func TestDone(t *testing.T) {
	t.Run("with retry, the value goes back in line at the end", func(t *testing.T) {
		q := newQueue()
		q.Put(save{"a", 1})
		q.Put(save{"b", 1})
		take(t, q, save{"a", 1})
		q.Done(save{"a", 1}, true)
		take(t, q, save{"b", 1})
		take(t, q, save{"a", 1})
	})
	t.Run("a newer value put meanwhile wins over a retry", func(t *testing.T) {
		q := newQueue()
		q.Put(save{"a", 1})
		take(t, q, save{"a", 1})
		q.Put(save{"a", 2})
		q.Done(save{"a", 1}, true)
		take(t, q, save{"a", 2})
		empty(t, q)
	})
	t.Run("without retry, the value is gone", func(t *testing.T) {
		q := newQueue()
		q.Put(save{"a", 1})
		take(t, q, save{"a", 1})
		q.Done(save{"a", 1}, false)
		empty(t, q)
		if v, ok := q.Get("a"); ok {
			t.Fatalf("Get = %v", v)
		}
	})
	t.Run("for a key not taken, nothing happens", func(t *testing.T) {
		q := newQueue()
		q.Put(save{"a", 1})
		q.Done(save{"a", 1}, true)
		take(t, q, save{"a", 1})
		empty(t, q)
	})
}

func TestGetIsTheNewestValueWaitingOrTaken(t *testing.T) {
	q := newQueue()
	get := func(want save, has bool) {
		t.Helper()
		if v, ok := q.Get("a"); ok != has || v != want {
			t.Fatalf("Get = %v, %t; want %v, %t", v, ok, want, has)
		}
	}
	get(save{}, false)
	q.Put(save{"a", 1})
	get(save{"a", 1}, true)
	take(t, q, save{"a", 1})
	get(save{"a", 1}, true)
	q.Put(save{"a", 2})
	get(save{"a", 2}, true)
	q.Done(save{"a", 1}, false)
	get(save{"a", 2}, true)
}

func TestReadyIsSignalledWhenThereIsSomethingToTake(t *testing.T) {
	q := newQueue()
	ready := func() bool {
		select {
		case <-q.Ready():
			return true
		default:
			return false
		}
	}
	if ready() {
		t.Fatal("ready before any put")
	}
	q.Put(save{"a", 1})
	q.Put(save{"b", 1})
	if !ready() || ready() {
		t.Fatal("puts are signalled once until received")
	}
	take(t, q, save{"a", 1})
	q.Done(save{"a", 1}, true)
	if !ready() {
		t.Fatal("a retry is not signalled")
	}
}

// Puts landing while consumers sit between an empty Take and Ready wake all of them: each taker passes the signal on.
func TestABurstWakesEveryWaitingConsumer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		q := newQueue()
		var mu sync.Mutex
		var took []save
		gap, hold := make(chan struct{}), make(chan struct{})
		defer close(hold)
		for range 3 {
			go func() {
				if _, ok := q.Take(); ok {
					t.Error("took from an empty queue")
				}
				<-gap
				select {
				case <-q.Ready():
				case <-hold:
					return
				}
				if s, ok := q.Take(); ok {
					mu.Lock()
					took = append(took, s)
					mu.Unlock()
				}
				<-hold
			}()
		}
		synctest.Wait()
		q.Put(save{"a", 1})
		q.Put(save{"b", 1})
		q.Put(save{"c", 1})
		close(gap)
		synctest.Wait()
		if len(took) != 3 {
			t.Fatalf("took %v while three consumers waited", took)
		}
	})
}

// Many producers and one consumer: each key's values come out in the order put, and its last one always does.
func TestConcurrentPutsComeOutInOrder(t *testing.T) {
	q := newQueue()
	const producers, puts = 8, 1000
	var wg sync.WaitGroup
	for p := range producers {
		key := string(rune('a' + p))
		wg.Go(func() {
			for n := 1; n <= puts; n++ {
				q.Put(save{key, n})
			}
		})
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	last := map[string]int{}
	consume := func() {
		for {
			s, ok := q.Take()
			if !ok {
				return
			}
			if s.n <= last[s.key] {
				t.Errorf("%s: %d after %d", s.key, s.n, last[s.key])
			}
			last[s.key] = s.n
			q.Done(s, false)
		}
	}
	for running := true; running; {
		select {
		case <-q.Ready():
		case <-done:
			running = false
		}
		consume()
	}
	consume()
	for p := range producers {
		if key := string(rune('a' + p)); last[key] != puts {
			t.Errorf("%s: last value %d, want %d", key, last[key], puts)
		}
	}
}
