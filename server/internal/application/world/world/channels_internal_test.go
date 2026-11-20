package world

import (
	"slices"
	"testing"
	"time"

	"finga/internal/application/world/instance"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
)

// testRegion is a region whose channels are numbered ns, each with pending players on its way: an instance never run has none in it.
func testRegion(pending map[int]int, ns ...int) *region {
	r := &region{m: &gamedata.Map{ID: "vale"}}
	for _, n := range ns {
		r.open = append(r.open, &channel{n: n, inst: &instance.Instance{}, id: channelID(r.m, n), pending: pending[n]})
	}
	return r
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
			cs := &channels{cap: 2, regions: map[string]*region{"vale": r}}
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
	cs := &channels{cap: 2, regions: map[string]*region{"vale": r}}
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
	cs := &channels{cap: 2, idle: idle, regions: map[string]*region{"vale": r}}
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
	if ok, _ := cs.touch(r, 2); ok {
		t.Error("touched channel 2 after it closed")
	}
	if got := cs.loads(r); !slices.Equal(got.Open, []model.ChannelLoad{{Channel: 1}}) || got.Cap != 2 {
		t.Errorf("loads %+v", got)
	}
}
