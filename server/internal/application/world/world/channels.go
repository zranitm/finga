package world

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"finga/internal/application/world/instance"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/pkg/panics"
	"finga/pkg/transient"
)

// channelSweep is how often the World looks for channels to close.
const channelSweep = 10 * time.Second

// channels are each region's channels (large-regions.md section 4): its first, a fixed map's instance that never closes, and those opened as the rest fill.
type channels struct {
	mu       sync.Mutex
	cap      int           // players a channel takes before a pick passes it; a switch may go past it
	idle     time.Duration // how long a channel past the first stays open empty
	regions  map[string]*region
	observer ChannelObserver
}

// region is a region's open channels, by number.
type region struct {
	m    *gamedata.Map
	open []*channel
}

// channel is one of a region's instances, and the hand-offs picked for it that haven't landed.
type channel struct {
	n       int
	inst    *instance.Instance
	id      string // its id in opened; the first's is the map's
	pending int
	empty   time.Time // when a sweep first found it empty; zero while anyone's in it or coming
}

// load is the players on c and on their way.
func (c *channel) load() int { return c.inst.Players() + c.pending }

// pick is the channel of r a hand-off goes to, one more on its way to it until release:
//   - want, while it's open and has room, or, on a switch, open;
//   - else the one holding most of the player's party, by channel number in party, that has room;
//   - else the least full with room;
//   - else a new one, with the lowest free number, made by open.
//
// Ties go to the lowest number.
func (cs *channels) pick(r *region, want int, over bool, party map[int]int, open func(m *gamedata.Map, n int) (*channel, error)) (*channel, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	room := func(c *channel) bool { return c.load() < cs.cap }
	var to *channel
	if i := slices.IndexFunc(r.open, func(c *channel) bool { return c.n == want }); i >= 0 && (over || room(r.open[i])) {
		to = r.open[i]
	}
	if to == nil {
		for _, c := range r.open {
			if room(c) && party[c.n] > 0 && (to == nil || party[c.n] > party[to.n]) {
				to = c
			}
		}
	}
	if to == nil {
		for _, c := range r.open {
			if room(c) && (to == nil || c.load() < to.load()) {
				to = c
			}
		}
	}
	if to == nil {
		n := 1
		for _, c := range r.open {
			if c.n == n {
				n++
			}
		}
		c, err := open(r.m, n)
		if err != nil {
			return nil, err
		}
		r.open = append(r.open, c)
		slices.SortFunc(r.open, func(a, b *channel) int { return cmp.Compare(a.n, b.n) })
		cs.observer.ChannelsOpen(r.m.ID, len(r.open))
		to = c
	}
	to.pending++
	to.empty = time.Time{}
	return to, nil
}

// release counts a hand-off picked for c as landed or failed.
func (cs *channels) release(c *channel) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	c.pending--
}

// has reports whether channel n of r is open, and has room.
func (cs *channels) has(r *region, n int) (open, room bool) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if i := slices.IndexFunc(r.open, func(c *channel) bool { return c.n == n }); i >= 0 {
		return true, r.open[i].load() < cs.cap
	}
	return false, false
}

// touch restarts channel n of r's idle clock, if it's open, so a switch to it finds it open at its end.
func (cs *channels) touch(r *region, n int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if i := slices.IndexFunc(r.open, func(c *channel) bool { return c.n == n }); i >= 0 {
		r.open[i].empty = time.Time{}
	}
}

// sweep takes off the channels past the first found empty for idle by now, and gives their ids in opened to shut; none can be picked once taken off.
func (cs *channels) sweep(now time.Time) []string {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	var shut []string
	for _, r := range cs.regions {
		was := len(r.open)
		r.open = slices.DeleteFunc(r.open, func(c *channel) bool {
			switch {
			case c.n == 1 || c.load() > 0:
				c.empty = time.Time{}
			case c.empty.IsZero():
				c.empty = now
			case now.Sub(c.empty) >= cs.idle:
				shut = append(shut, c.id)
				return true
			}
			return false
		})
		if len(r.open) != was {
			cs.observer.ChannelsOpen(r.m.ID, len(r.open))
		}
	}
	return shut
}

// stopped tells the observer channel n of region m has run its last tick, unless a channel n has opened again since, so an old one's end says nothing of the new.
func (cs *channels) stopped(m *gamedata.Map, n int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if !slices.ContainsFunc(cs.regions[m.ID].open, func(c *channel) bool { return c.n == n }) {
		cs.observer.ChannelStopped(m.ID, n)
	}
}

// loads are r's open channels' numbers and players, on their way included.
func (cs *channels) loads(r *region) model.Channels {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	out := model.Channels{Open: make([]model.ChannelLoad, 0, len(r.open)), Cap: cs.cap}
	for _, c := range r.open {
		out.Open = append(out.Open, model.ChannelLoad{Channel: c.n, Players: c.load()})
	}
	return out
}

// channelID is channel n of region m's id in opened.
func channelID(m *gamedata.Map, n int) string { return fmt.Sprintf("%s/%d", m.ID, n) }

// SwitchChannel starts a switch to channel n of the character's region, pressed during input seq, once n is open with room, and once the map takes it restarts n's idle clock so it stays open for it; the switch then channels as a teleport does, and lands past the cap if n filled meanwhile.
func (w *World) SwitchChannel(ctx context.Context, id kernel.CharacterID, conn string, n int, seq uint32) error {
	return w.wait(id, conn, func(i *instance.Instance) error {
		r, ok := w.channels.regions[i.MapID()]
		if !ok {
			return model.ErrNotARegion
		}
		if n == i.Channel() {
			return model.ErrSameChannel
		}
		switch open, room := w.channels.has(r, n); {
		case !open:
			return model.ErrNoSuchChannel
		case !room:
			return model.ErrChannelFull
		}
		if err := i.SwitchChannel(ctx, id, conn, n, seq); err != nil {
			return err
		}
		w.channels.touch(r, n)
		return nil
	})
}

// Channels are the open channels of the region the character is in, and how full each is; ErrNotARegion off one.
func (w *World) Channels(id kernel.CharacterID, conn string) (model.Channels, error) {
	at, ok := w.presences.at(id, conn)
	if !ok {
		return model.Channels{}, fmt.Errorf("%w: the connection doesn't hold the character", transient.ErrUnavailable)
	}
	r, ok := w.channels.regions[at.inst.MapID()]
	if !ok {
		return model.Channels{}, model.ErrNotARegion
	}
	return w.channels.loads(r), nil
}

// Bosses is how each hunting area's field boss stands on the region channel the character is in, in its areas' order; ErrNotARegion off one.
func (w *World) Bosses(ctx context.Context, id kernel.CharacterID, conn string) ([]model.AreaBoss, error) {
	var out []model.AreaBoss
	err := w.wait(id, conn, func(i *instance.Instance) error {
		if _, ok := w.channels.regions[i.MapID()]; !ok {
			return model.ErrNotARegion
		}
		got, err := i.Bosses(ctx)
		out = got
		return err
	})
	return out, err
}

// pickChannel is the channel of r character id goes to, as channels.pick picks it, with their party's channels in r, and what to call once they've landed or failed.
func (w *World) pickChannel(id kernel.CharacterID, r *region, want int, over bool) (*instance.Instance, func(), error) {
	party := map[int]int{}
	for _, m := range w.deps.Parties.Party(id).Members {
		if at, ok := w.presences.get(m); ok && m != id && at.inst.MapID() == r.m.ID {
			party[at.inst.Channel()]++
		}
	}
	c, err := w.channels.pick(r, want, over, party, w.openChannel)
	if err != nil {
		return nil, nil, err
	}
	return c.inst, func() { w.channels.release(c) }, nil
}

// openChannel starts channel n of region m.
func (w *World) openChannel(m *gamedata.Map, n int) (*channel, error) {
	inst := instance.New(instance.Deps{Services: w.deps.Services, Map: m, Data: w.data, Saves: w.saves, Mover: w, Logger: w.deps.Logger, Channel: n}, w.tuning)
	id := channelID(m, n)
	if err := w.opened.run(id, inst, func() { w.channels.stopped(m, n) }); err != nil {
		return nil, err
	}
	return &channel{n: n, inst: inst, id: id}, nil
}

// closeIdleChannels shuts, every channelSweep until the World stops, the channels past the first left empty for the idle time; a pass that panics is recorded, and the next runs.
func (w *World) closeIdleChannels() {
	t := time.NewTicker(channelSweep)
	defer t.Stop()
	for {
		select {
		case <-w.jobsCtx.Done():
			return
		case now := <-t.C:
			w.deps.Panics.Do(w.jobsCtx, panics.FlowJob, func() {
				for _, id := range w.channels.sweep(now) {
					w.opened.shut(id)
				}
			}, "job", "close idle channels")
		}
	}
}
