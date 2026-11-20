package world

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"go.opentelemetry.io/otel/trace/noop"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/itemcalls"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/kernel"
	"finga/pkg/panics"
)

// testRegion is a region whose channels are numbered ns, each with pending players on its way: an instance never run has none in it.
func testRegion(pending map[int]int, ns ...int) *region {
	r := &region{m: &gamedata.Map{ID: "vale"}}
	for _, n := range ns {
		r.open = append(r.open, &channel{n: n, inst: &instance.Instance{}, id: channelID(r.m, n), pending: pending[n]})
	}
	return r
}

// channelsOf are channels over r, and the open counts they told.
func channelsOf(capacity int, idle time.Duration, r *region) (*channels, *[]int) {
	heard := &[]int{}
	return &channels{cap: capacity, idle: idle, regions: map[string]*region{"vale": r}, observe: func(region string, open int) {
		if region == "vale" {
			*heard = append(*heard, open)
		}
	}}, heard
}

// opens makes channels as the World does, recording their numbers.
func opens(made *[]int) func(*gamedata.Map, int) (*channel, error) {
	return func(m *gamedata.Map, n int) (*channel, error) {
		*made = append(*made, n)
		return &channel{n: n, inst: &instance.Instance{}, id: channelID(m, n)}, nil
	}
}

func numbers(r *region) []int {
	var out []int
	for _, c := range r.open {
		out = append(out, c.n)
	}
	return out
}

// A pick (large-regions.md section 4): the channel asked for while it has room, or on a switch; else the one holding most of the party with room; else the least full with room; else a new one with the lowest free number. Ties go to the lowest number.
func TestAPickFollowsTheChannelRules(t *testing.T) {
	for _, c := range []struct {
		what    string
		pending map[int]int // players in or coming to each channel, of 2 a channel takes
		open    []int
		want    int
		over    bool
		party   map[int]int
		got     int
		made    []int
	}{
		{what: "the least full", pending: map[int]int{1: 1, 2: 0, 3: 1}, open: []int{1, 2, 3}, got: 2},
		{what: "a tie to the lowest", pending: map[int]int{1: 2, 2: 1, 3: 1}, open: []int{1, 2, 3}, got: 2},
		{what: "a new one once all are full", pending: map[int]int{1: 2, 2: 2}, open: []int{1, 2}, got: 3, made: []int{3}},
		{what: "the lowest free number, one closed below", pending: map[int]int{1: 2, 3: 2}, open: []int{1, 3}, got: 2, made: []int{2}},
		{what: "the channel asked for, though fuller", pending: map[int]int{1: 1, 2: 0}, open: []int{1, 2}, want: 1, got: 1},
		{what: "the channel asked for, full: the pick instead", pending: map[int]int{1: 2, 2: 0}, open: []int{1, 2}, want: 1, got: 2},
		{what: "a switch, past the cap", pending: map[int]int{1: 2, 2: 0}, open: []int{1, 2}, want: 1, over: true, got: 1},
		{what: "a channel asked for that closed: the pick", pending: map[int]int{1: 1}, open: []int{1}, want: 2, over: true, got: 1},
		{what: "the party's, though fuller", pending: map[int]int{1: 1, 2: 0}, open: []int{1, 2}, party: map[int]int{1: 1}, got: 1},
		{what: "the party's with most of it", pending: map[int]int{1: 1, 2: 1, 3: 0}, open: []int{1, 2, 3}, party: map[int]int{1: 1, 2: 1, 3: 0}, got: 1},
		{what: "the party's with most of it, past a smaller share", pending: map[int]int{1: 1, 2: 0, 3: 0}, open: []int{1, 2, 3}, party: map[int]int{1: 1, 2: 0, 3: 2}, got: 3},
		{what: "the party's biggest share full: the next share", pending: map[int]int{1: 2, 2: 1, 3: 0}, open: []int{1, 2, 3}, party: map[int]int{1: 2, 2: 1}, got: 2},
		{what: "the party's only channel full: the least full", pending: map[int]int{1: 2, 2: 1, 3: 0}, open: []int{1, 2, 3}, party: map[int]int{1: 2}, got: 3},
	} {
		t.Run(c.what, func(t *testing.T) {
			r := testRegion(c.pending, c.open...)
			cs, heard := channelsOf(2, 0, r)
			var made []int
			got, err := cs.pick(r, c.want, c.over, c.party, opens(&made))
			if err != nil {
				t.Fatal(err)
			}
			if got.n != c.got || !slices.Equal(made, c.made) {
				t.Fatalf("picked %d, opened %v; want %d, opening %v", got.n, made, c.got, c.made)
			}
			if got.pending != c.pending[got.n]+1 {
				t.Errorf("%d on their way to it; want one more than %d", got.pending, c.pending[got.n])
			}
			if len(c.made) > 0 && !slices.Equal(*heard, []int{len(c.open) + 1}) {
				t.Errorf("heard %v open; want %d", *heard, len(c.open)+1)
			}
			if !slices.IsSorted(numbers(r)) {
				t.Errorf("open %v, out of order", numbers(r))
			}
			cs.release(got)
			if got.pending != c.pending[got.n] {
				t.Errorf("%d on their way after the landing; want %d", got.pending, c.pending[got.n])
			}
		})
	}
}

// One on its way counts: two picks before either lands don't both take the last place.
func TestAPickCountsThoseOnTheirWay(t *testing.T) {
	r := testRegion(map[int]int{1: 1}, 1)
	cs, _ := channelsOf(2, 0, r)
	var made []int
	a, _ := cs.pick(r, 0, false, nil, opens(&made))
	b, _ := cs.pick(r, 0, false, nil, opens(&made))
	if a.n != 1 || b.n != 2 {
		t.Fatalf("picked %d then %d; want 1 then a new 2", a.n, b.n)
	}
}

// A channel past the first closes once a sweep has found it empty, none on their way, for the idle time; the first never does, and a pick or a touch restarts the clock.
func TestAChannelPastTheFirstClosesOnceIdle(t *testing.T) {
	const idle = 5 * time.Minute
	t0 := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	r := testRegion(map[int]int{2: 1, 3: 0}, 1, 2, 3)
	cs, heard := channelsOf(2, idle, r)
	two, three := r.open[1], r.open[2]
	sweep := func(at time.Duration) []string { return cs.sweep(t0.Add(at)) }
	if shut := sweep(0); len(shut) != 0 || !two.empty.IsZero() || !three.empty.Equal(t0) {
		t.Fatalf("first sweep shut %v; 2 empty since %v, 3 since %v", shut, two.empty, three.empty)
	}
	cs.release(two) // the one on their way to 2 landed, and left
	sweep(10 * time.Second)
	if ok, room := cs.touch(r, 3); !ok || !room {
		t.Fatal("3 isn't open with room")
	}
	sweep(20 * time.Second)
	if shut := sweep(10*time.Second + idle - time.Nanosecond); len(shut) != 0 {
		t.Fatalf("shut %v a nanosecond early", shut)
	}
	if shut := sweep(10*time.Second + idle); !slices.Equal(shut, []string{"vale/2"}) || !slices.Equal(numbers(r), []int{1, 3}) {
		t.Fatalf("shut %v, left %v; want vale/2 shut", shut, numbers(r))
	}
	if shut := sweep(20*time.Second + idle); !slices.Equal(shut, []string{"vale/3"}) || !slices.Equal(numbers(r), []int{1}) {
		t.Fatalf("shut %v, left %v; want vale/3 shut, its clock restarted by the touch", shut, numbers(r))
	}
	if shut := sweep(24 * time.Hour); len(shut) != 0 || !slices.Equal(numbers(r), []int{1}) {
		t.Fatalf("shut %v; the first channel never closes", shut)
	}
	if !slices.Equal(*heard, []int{2, 1}) {
		t.Errorf("heard %v open; want 2 then 1", *heard)
	}
	if ok, _ := cs.touch(r, 2); ok {
		t.Error("touched channel 2 after it closed")
	}
	if got := cs.loads(r); !slices.Equal(got.Open, []model.ChannelLoad{{Channel: 1}}) || got.Cap != 2 {
		t.Errorf("loads %+v", got)
	}
}

// stall holds channel 2's next tick, once armed, until hold closes: an instance caught winding down.
type stall struct {
	mu    sync.Mutex
	armed bool
	hold  chan struct{}
}

func (s *stall) ObserveTick(_ string, channel int, _, _ time.Duration) {
	s.mu.Lock()
	held := s.armed && channel == 2
	if held {
		s.armed = false
	}
	s.mu.Unlock()
	if held {
		<-s.hold
	}
}

// A channel's number opens again the moment its sweep shut it, its old instance still winding down: a pick with every other channel full takes it, and the old one's end forgets nothing of the new.
func TestAShutChannelsNumberOpensAgainAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := gamedatatest.Shipped(t)
		rec := panics.NewRecorder(slog.New(slog.DiscardHandler), nil)
		ticks := &stall{hold: make(chan struct{})}
		w := New(Deps{
			NewRoll: func() model.Roll { return half{} }, Observer: ticks, Parties: alone{}, Items: nothing{}, Batches: nothing{}, ItemCalls: itemcalls.NewPool(itemcalls.Deps{Observer: quiet{}, Panics: rec}, itemcalls.Options{Workers: 1}), Traces: noop.NewTracerProvider(),
			Data: data, Saver: nothing{}, Characters: nothing{}, Starter: nothing{}, SaveObserver: nothing{}, Channels: nothing{}, Panics: rec, Logger: slog.New(slog.DiscardHandler),
		}, Tuning{TickInterval: 50 * time.Millisecond, CheckpointInterval: time.Hour, FlushTimeout: time.Second, BatchMax: 32, ChannelCap: 1, ChannelIdle: time.Minute})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = w.Run(ctx) }()
		defer func() { cancel(); <-done }()
		release := sync.OnceFunc(func() { close(ticks.hold) })
		defer release() // first, on any failure, so the World can stop
		time.Sleep(50 * time.Millisecond)
		synctest.Wait()

		r := w.channels.regions["test_region"]
		r.open[0].pending = 1 // channel 1 full
		_, landed, err := w.pickChannel(kernel.NewCharacterID(), r, 0, false)
		if err != nil {
			t.Fatal(err)
		}
		landed()
		old, _ := w.opened.get("test_region/2")
		ticks.mu.Lock()
		ticks.armed = true
		ticks.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		synctest.Wait() // channel 2 is held inside a tick
		now := time.Now()
		w.channels.sweep(now)
		shut := w.channels.sweep(now.Add(time.Minute))
		if !slices.Equal(shut, []string{"test_region/2"}) {
			t.Fatalf("shut %v", shut)
		}
		w.opened.shut(shut[0])
		inst, landed, err := w.pickChannel(kernel.NewCharacterID(), r, 0, false)
		if err != nil {
			t.Fatalf("channel 2 opening again while the old one winds down: %v", err)
		}
		landed()
		release()
		synctest.Wait() // the old one has ended
		if got, ok := w.opened.get("test_region/2"); !ok || got != inst || got == old || inst.Channel() != 2 {
			t.Fatalf("open under test_region/2: %p (%v); want the new channel %p, not the old %p", got, ok, inst, old)
		}
	})
}
