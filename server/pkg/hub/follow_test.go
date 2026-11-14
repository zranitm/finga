package hub_test

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/pkg/hub"
	"finga/pkg/panics"
)

// flows counts recorded panics by flow.
type flows struct {
	mu sync.Mutex
	n  map[string]int
}

func (f *flows) Inc(flow string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n[flow]++
}

func TestFollowHandsOverWhatSyncSendsThenEachEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := hub.New[string](4)
		ctx, cancel := context.WithCancel(t.Context())
		var got []string
		go hub.Follow(ctx, func() *hub.Sub[string] { return h.Subscribe("w") }, func(context.Context) error {
			got = append(got, "subscribed")
			return nil
		}, func(s string) { got = append(got, s) }, hub.FollowOptions{})
		synctest.Wait()
		h.Broadcast("a")
		h.Broadcast("b")
		synctest.Wait()
		cancel()
		synctest.Wait()
		if !slices.Equal(got, []string{"subscribed", "a", "b"}) {
			t.Fatalf("handled %v", got)
		}
	})
}

func TestFollowSubscribesAgainAfterAClose(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := hub.New[string](1)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		opened := 0
		var breaks []error
		go hub.Follow(ctx, func() *hub.Sub[string] {
			opened++
			return h.Subscribe("w")
		}, nil, func(string) {}, hub.FollowOptions{OnBreak: func(err error) { breaks = append(breaks, err) }})
		synctest.Wait()
		sub, _ := h.Get("w")
		sub.Close() // as a full queue closes it
		synctest.Wait()
		time.Sleep(2 * time.Second) // past the longest backoff
		synctest.Wait()
		if opened != 2 || len(breaks) != 1 || !errors.Is(breaks[0], hub.ErrClosed) {
			t.Fatalf("opened %d, breaks %v; want a second subscription after one ErrClosed", opened, breaks)
		}
	})
}

func TestFollowBacksOffASyncThatFailsAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := hub.New[string](1)
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		opened := &subs{}
		hub.Follow(ctx, func() *hub.Sub[string] { return opened.add(h.Subscribe("w")) },
			func(context.Context) error { return errors.New("not yet") }, func(string) {}, hub.FollowOptions{})
		// Each wait doubles from 10 ms toward 1 s: about 7 tries reach it, then one a second; without the backoff, a thousand.
		if n := len(opened.get()); n > 20 {
			t.Fatalf("%d subscriptions in ten seconds: the backoff never grew", n)
		}
		for _, sub := range opened.get() {
			select {
			case <-sub.Done():
			default:
				t.Fatal("a subscription whose sync failed is open")
			}
		}
	})
}

// subs are the subscriptions a follower opened, read while it goes on.
type subs struct {
	mu   sync.Mutex
	list []*hub.Sub[string]
}

func (s *subs) add(sub *hub.Sub[string]) *hub.Sub[string] {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.list = append(s.list, sub)
	return sub
}

func (s *subs) get() []*hub.Sub[string] {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.list)
}

// A panic handing over what comes first closes the subscription at once: the producer queues to nobody until the next.
func TestAPanicInSyncClosesTheSubscription(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := hub.New[string](4)
		count := &flows{n: map[string]int{}}
		rec := panics.NewRecorder(slog.New(slog.DiscardHandler), count)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		opened := &subs{}
		go hub.Follow(ctx, func() *hub.Sub[string] { return opened.add(h.Subscribe("w")) }, func(context.Context) error {
			if len(opened.get()) == 1 {
				panic("hand-over bug")
			}
			return nil
		}, func(string) {}, hub.FollowOptions{Panics: rec})
		synctest.Wait()
		select {
		case <-opened.get()[0].Done():
		default:
			t.Fatal("the subscription the panic left is open")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		count.mu.Lock()
		defer count.mu.Unlock()
		if n := len(opened.get()); n != 2 || count.n[panics.FlowStream] != 1 {
			t.Fatalf("%d subscriptions, %v recorded; want a second after one stream panic", n, count.n)
		}
	})
}

func TestAPanicHandlingAnEventBreaksTheSubscriptionAndIsRecorded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := hub.New[string](4)
		count := &flows{n: map[string]int{}}
		rec := panics.NewRecorder(slog.New(slog.DiscardHandler), count)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		opened := 0
		var broke error
		go hub.Follow(ctx, func() *hub.Sub[string] {
			opened++
			return h.Subscribe("w")
		}, nil, func(s string) {
			if s == "bad" {
				panic("handler bug")
			}
		}, hub.FollowOptions{Panics: rec, OnBreak: func(err error) { broke = err }})
		synctest.Wait()
		h.Broadcast("bad")
		synctest.Wait()
		time.Sleep(time.Second)
		synctest.Wait()
		if _, ok := errors.AsType[*panics.Error](broke); !ok || opened != 2 || count.n[panics.FlowStream] != 1 {
			t.Fatalf("broke with %v, opened %d, %v recorded; want the panic, a second subscription and one stream panic", broke, opened, count.n)
		}
		if _, ok := h.Get("w"); !ok {
			t.Fatal("no subscription after the panic")
		}
	})
}

// opens records when a follower subscribes, and waits for its next subscription on synctest's clock.
type opens struct {
	mu sync.Mutex
	at []time.Time
}

func (o *opens) subscribe(h *hub.Hub[string]) func() *hub.Sub[string] {
	return func() *hub.Sub[string] {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.at = append(o.at, time.Now())
		return h.Subscribe("w")
	}
}

// reach waits until the follower has subscribed n times, and answers when the last one opened.
func (o *opens) reach(n int) time.Time {
	for {
		synctest.Wait()
		o.mu.Lock()
		if len(o.at) >= n {
			defer o.mu.Unlock()
			return o.at[n-1]
		}
		o.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
}

// After quick breaks grow the wait, a subscription that held past the longest wait starts it over: no crash loop is under way.
func TestFollowStartsOverAfterASubscriptionThatHeld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := hub.New[string](1)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		o := &opens{}
		go hub.Follow(ctx, o.subscribe(h), nil, func(string) {}, hub.FollowOptions{})
		o.reach(1)
		cut := func() time.Time {
			synctest.Wait()
			sub, _ := h.Get("w")
			sub.Close()
			return time.Now()
		}
		var grown time.Duration
		for n := 2; n <= 6; n++ { // five quick breaks: the fifth wait is 160 ms, give or take half
			at := cut()
			grown = o.reach(n).Sub(at)
		}
		time.Sleep(3 * time.Second) // the sixth subscription holds past the longest wait
		at := cut()
		if wait := o.reach(7).Sub(at); grown < 50*time.Millisecond || wait > 20*time.Millisecond {
			t.Fatalf("waited %s after quick breaks and %s after one that held; want the wait grown, then started over at 10 ms", grown, wait)
		}
	})
}

// A follower whose context ends closes its open subscription, so the producer stops queueing to it.
func TestFollowClosesItsSubscriptionWhenItsContextEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := hub.New[string](1)
		ctx, cancel := context.WithCancel(t.Context())
		o := &opens{}
		done := make(chan struct{})
		go func() {
			hub.Follow(ctx, o.subscribe(h), nil, func(string) {}, hub.FollowOptions{})
			close(done)
		}()
		o.reach(1)
		sub, ok := h.Get("w")
		if !ok {
			t.Fatal("no subscription open")
		}
		cancel()
		synctest.Wait()
		select {
		case <-sub.Done():
		default:
			t.Fatal("the subscription is still open after its follower's context ended")
		}
		select {
		case <-done:
		default:
			t.Fatal("Follow is still running")
		}
	})
}
