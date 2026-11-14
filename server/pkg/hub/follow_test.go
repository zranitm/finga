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

func TestFollowHandsOverWhatOpenSendsThenEachEvent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		h := hub.New[string](4)
		ctx, cancel := context.WithCancel(t.Context())
		var got []string
		go hub.Follow(ctx, func(context.Context) (*hub.Sub[string], error) {
			sub := h.Subscribe("w")
			got = append(got, "subscribed")
			return sub, nil
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
		go hub.Follow(ctx, func(context.Context) (*hub.Sub[string], error) {
			opened++
			return h.Subscribe("w"), nil
		}, func(string) {}, hub.FollowOptions{OnBreak: func(err error) { breaks = append(breaks, err) }})
		synctest.Wait()
		sub, _ := h.Get("w")
		sub.Close() // as a full queue closes it
		synctest.Wait()
		time.Sleep(time.Second) // past the longest backoff
		synctest.Wait()
		if opened != 2 || len(breaks) != 1 || !errors.Is(breaks[0], hub.ErrClosed) {
			t.Fatalf("opened %d, breaks %v; want a second subscription after one ErrClosed", opened, breaks)
		}
	})
}

func TestFollowBacksOffASubscriptionThatFailsAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		opened := 0
		hub.Follow(ctx, func(context.Context) (*hub.Sub[string], error) {
			opened++
			return nil, errors.New("not yet")
		}, func(string) {}, hub.FollowOptions{})
		// Each wait doubles from 10 ms toward 1 s: about 7 tries reach it, then one a second; without the backoff, a thousand.
		if opened > 20 {
			t.Fatalf("%d subscriptions in ten seconds: the backoff never grew", opened)
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
		go hub.Follow(ctx, func(context.Context) (*hub.Sub[string], error) {
			opened++
			return h.Subscribe("w"), nil
		}, func(s string) {
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
