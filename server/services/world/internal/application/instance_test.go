package application_test

import (
	"cmp"
	"context"
	"log/slog"
	"math"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/pkg/panics"
	"finga/services/world/internal/application"
	"finga/services/world/internal/model"
)

const tickInterval = 50 * time.Millisecond

type sink struct {
	mu     sync.Mutex
	events []model.Event
	full   bool // when set, Send refuses: a slow consumer
}

func (s *sink) Send(e model.Event) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.full {
		return false
	}
	s.events = append(s.events, e)
	return true
}

func (s *sink) all() []model.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]model.Event(nil), s.events...)
}

func (s *sink) deltas() []model.Delta {
	var out []model.Delta
	for _, e := range s.all() {
		if d, ok := e.(model.Delta); ok {
			out = append(out, d)
		}
	}
	return out
}

func (s *sink) last() model.Delta {
	ds := s.deltas()
	if len(ds) == 0 {
		return model.Delta{Frame: &model.Frame{}}
	}
	return ds[len(ds)-1]
}

// own finds the player's own view in the latest delta that carries it.
func (s *sink) own(id kernel.CharacterID) (model.PlayerView, bool) {
	ds := s.deltas()
	for _, d := range slices.Backward(ds) {
		for _, p := range d.Players {
			if p.ID == id {
				return p, true
			}
		}
	}
	return model.PlayerView{}, false
}

type observer struct {
	mu    sync.Mutex
	ticks []time.Duration
}

func (o *observer) ObserveTick(_ string, took, _ time.Duration) {
	o.mu.Lock()
	o.ticks = append(o.ticks, took)
	o.mu.Unlock()
}

func loadData(t *testing.T) *gamedata.Data {
	t.Helper()
	d, err := gamedata.Load("../../../../data")
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func townSpawn(t *testing.T, data *gamedata.Data) (float64, float64) {
	t.Helper()
	town := data.Town()
	return town.PlayerSpawn.X, town.PlayerSpawn.Y
}

func newPlayer(name string, x, y float64) model.Player {
	return model.Player{ID: kernel.NewCharacterID(), Name: name, Class: kernel.Warrior, Level: 1, Stats: gamedata.Stats{STR: 12, DEX: 5, INT: 4, AGI: 5, VIT: 10}, HP: 204, MP: 28, X: x, Y: y}
}

// rolls gives each instance a fixedRoll of v.
func rolls(v float64) func() model.Roll {
	return func() model.Roll { return fixedRoll{v} }
}

func discardPanics() *panics.Recorder { return panics.NewRecorder(slog.New(slog.DiscardHandler), nil) }

// options are what a test sets; fill gives the rest a 50 ms tick, rolls of 0.5 and services that do nothing.
type options struct {
	TickInterval       time.Duration
	CheckpointInterval time.Duration
	FlushTimeout       time.Duration
	NewRoll            func() model.Roll
	Observer           application.TickObserver
	Transfer           application.Mover // an instance's; the World moves players itself
	Items              application.Pickups
	Workers            *application.Workers
	Parties            application.Parties
	Characters         application.Characters
	Starter            application.Starter
	Logger             *slog.Logger // the World's
}

func (o options) fill() options {
	o.TickInterval = cmp.Or(o.TickInterval, tickInterval)
	o.CheckpointInterval = cmp.Or(o.CheckpointInterval, 30*time.Second)
	o.FlushTimeout = cmp.Or(o.FlushTimeout, 10*time.Second)
	if o.NewRoll == nil {
		o.NewRoll = rolls(0.5)
	}
	if o.Observer == nil {
		o.Observer = &observer{}
	}
	if o.Transfer == nil {
		o.Transfer = &transferRecorder{}
	}
	if o.Items == nil {
		o.Items = newFakePickups()
	}
	if o.Workers == nil {
		o.Workers = application.NewWorkers(1, &economy{}, discardPanics())
	}
	if o.Parties == nil {
		o.Parties = &fakeParties{}
	}
	if o.Characters == nil {
		o.Characters = &fakeCharacters{}
	}
	if o.Starter == nil {
		o.Starter = &fakeStarter{}
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	return o
}

func (o options) tuning() application.Tuning {
	return application.Tuning{TickInterval: o.TickInterval, CheckpointInterval: o.CheckpointInterval, FlushTimeout: o.FlushTimeout}
}

func (o options) deps() application.Deps {
	return application.Deps{NewRoll: o.NewRoll, Observer: o.Observer, Items: o.Items, Workers: o.Workers, Parties: o.Parties}
}

// checkpoints records what an instance puts for saving.
type checkpoints struct {
	mu  sync.Mutex
	cps []model.Checkpoint
}

func (c *checkpoints) Put(cp model.Checkpoint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cps = append(c.cps, cp)
}

// drain is what was put since the last drain.
func (c *checkpoints) drain() []model.Checkpoint {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := c.cps
	c.cps = nil
	return out
}

// newInstance builds m's instance with o filled in; cps may be nil.
func newInstance(m *gamedata.Map, data *gamedata.Data, cps *checkpoints, o options) *application.Instance {
	o = o.fill()
	if cps == nil {
		cps = &checkpoints{}
	}
	return application.NewInstance(m, data, cps, o.Transfer, o.tuning(), o.deps())
}

// newWorldOf builds a World over data with o filled in.
func newWorldOf(data *gamedata.Data, saver application.Saver, o options) *application.World {
	o = o.fill()
	return application.New(data, o.tuning(), application.WorldDeps{
		Deps: o.deps(), Saver: saver, Characters: o.Characters, Starter: o.Starter,
		Panics: discardPanics(), Logger: o.Logger,
	})
}

// run starts a town instance in the synctest bubble; stop waits for Run so nothing outlives it.
func run(t *testing.T, data *gamedata.Data, cps *checkpoints, opts options) (*application.Instance, func()) {
	t.Helper()
	return runMap(t, data, data.Town(), cps, opts)
}

// runMap starts m's instance in the synctest bubble, as run does the town's.
func runMap(t *testing.T, data *gamedata.Data, m *gamedata.Map, cps *checkpoints, opts options) (*application.Instance, func()) {
	t.Helper()
	inst := newInstance(m, data, cps, opts)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = inst.Run(ctx) }()
	var once sync.Once
	return inst, func() { once.Do(func() { cancel(); <-done }) }
}

func TestJoinSnapshotAndDeltas(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		x, y := townSpawn(t, data)
		inst, cancel := run(t, data, nil, options{})
		defer cancel()

		ayla, robin := newPlayer("Ayla", x, y), newPlayer("Robin", x+100, y)
		sa, sr := &sink{}, &sink{}
		if err := inst.Join(context.Background(), ayla, sa); err != nil {
			t.Fatal(err)
		}
		time.Sleep(tickInterval) // the join is handled on the next tick
		synctest.Wait()

		{ // a joining player gets a Snapshot first, then Deltas
			events := sa.all()
			if len(events) == 0 {
				t.Fatal("no events")
			}
			snap, ok := events[0].(model.Snapshot)
			if !ok || snap.MapID != "town" || len(snap.Players) != 1 || snap.Players[0].ID != ayla.ID || snap.Players[0].State.X != x {
				t.Fatalf("first event %+v", events[0])
			}
			if _, ok := events[1].(model.Delta); !ok {
				t.Fatalf("second event %T", events[1])
			}
		}

		{ // deltas arrive every tick
			before := len(sa.deltas())
			time.Sleep(10 * tickInterval)
			synctest.Wait()
			if got := len(sa.deltas()) - before; got != 10 {
				t.Fatalf("%d deltas in 10 ticks", got)
			}
		}

		{ // others see the join
			if err := inst.Join(context.Background(), robin, sr); err != nil {
				t.Fatal(err)
			}
			time.Sleep(tickInterval)
			synctest.Wait()
			found := false
			for _, d := range sa.deltas() {
				for _, p := range d.Players {
					found = found || (p.ID == robin.ID && p.Name == "Robin")
				}
			}
			if !found {
				t.Fatalf("no delta of Ayla's carries Robin: %+v", sa.last())
			}
			snap := sr.all()[0].(model.Snapshot)
			if len(snap.Players) != 2 {
				t.Fatalf("Robin's snapshot has %d players", len(snap.Players))
			}
		}

		{ // others see the leave
			_ = inst.LeaveConnection(robin.ID, "")
			time.Sleep(tickInterval)
			synctest.Wait()
			d := sa.last()
			if len(d.Removed) != 1 || d.Removed[0] != robin.ID {
				t.Fatalf("removed %v", d.Removed)
			}
			n := len(sr.all())
			time.Sleep(3 * tickInterval)
			synctest.Wait()
			if len(sr.all()) != n {
				t.Fatal("Robin kept receiving events after leaving")
			}
		}

		{ // a player standing still is not in the delta
			time.Sleep(20 * tickInterval) // long enough to land and settle
			synctest.Wait()
			if d := sa.last(); len(d.Players) != 0 {
				t.Fatalf("idle delta carries %d players", len(d.Players))
			}
		}
	})
}

func TestInputsApplyInSequenceOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		x, y := townSpawn(t, data)
		inst, cancel := run(t, data, nil, options{})
		defer cancel()
		p := newPlayer("Ayla", x, y)
		s := &sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(25 * tickInterval) // land and settle
		synctest.Wait()
		startX, _ := s.own(p.ID)
		if !startX.State.OnGround {
			t.Fatalf("not on the ground: %+v", startX.State)
		}

		{ // one input per tick moves at walk speed
			_ = inst.Input(p.ID, 1, sim.Input{Right: true})
			time.Sleep(tickInterval)
			synctest.Wait()
			v, _ := s.own(p.ID)
			want := x + 3*data.Movement().WalkSpeed*data.Movement().Dt
			if !near(v.State.X, want) {
				t.Fatalf("x = %v, want %v after one tick of walking", v.State.X, want)
			}
			if s.last().AckSeq != 1 {
				t.Fatalf("ack_seq %d, want 1", s.last().AckSeq)
			}
		}

		{ // two inputs in one tick apply over two ticks, in sequence order
			before, _ := s.own(p.ID)
			_ = inst.Input(p.ID, 3, sim.Input{})            // stop
			_ = inst.Input(p.ID, 2, sim.Input{Right: true}) // arrives out of order but is older
			time.Sleep(tickInterval)
			synctest.Wait()
			after, _ := s.own(p.ID)
			want := before.State.X + 3*data.Movement().WalkSpeed*data.Movement().Dt
			if !near(after.State.X, want) || s.last().AckSeq != 2 {
				t.Fatalf("x = %v ack %d, want %v and ack 2: the older input applies first", after.State.X, s.last().AckSeq, want)
			}
			time.Sleep(tickInterval)
			synctest.Wait()
			stopped, _ := s.own(p.ID)
			if stopped.State.X != after.State.X || s.last().AckSeq != 3 {
				t.Fatalf("x %v -> %v ack %d, want a stop with ack 3 on the next tick", after.State.X, stopped.State.X, s.last().AckSeq)
			}
		}

		{ // old and duplicate sequence numbers are ignored
			before, _ := s.own(p.ID)
			_ = inst.Input(p.ID, 2, sim.Input{Right: true}) // old
			_ = inst.Input(p.ID, 3, sim.Input{Left: true})  // duplicate seq
			time.Sleep(tickInterval)
			synctest.Wait()
			after, _ := s.own(p.ID)
			if after.State.X != before.State.X || s.last().AckSeq != 3 {
				t.Fatalf("state %+v ack %d; old inputs were applied", after.State, s.last().AckSeq)
			}
		}

		{ // one input moves one tick; with none queued the player waits
			before, _ := s.own(p.ID)
			_ = inst.Input(p.ID, 4, sim.Input{Left: true})
			time.Sleep(3 * tickInterval)
			synctest.Wait()
			after, _ := s.own(p.ID)
			want := before.State.X - 3*data.Movement().WalkSpeed*data.Movement().Dt
			if !near(after.State.X, want) {
				t.Fatalf("x = %v, want %v after one tick of walking left and two of waiting", after.State.X, want)
			}
		}

		{ // inputs for an unknown player are ignored
			if inst.Input(kernel.NewCharacterID(), 1, sim.Input{Right: true}) != nil {
				t.Fatal("the inbox refused")
			}
			time.Sleep(tickInterval)
			synctest.Wait()
		}
	})
}

// A player walks 200 px a second whatever the tick rate, as the client predicts.
func TestWalkingSpeedDoesNotDependOnTheTickRate(t *testing.T) {
	for _, interval := range []time.Duration{50 * time.Millisecond, 100 * time.Millisecond} {
		t.Run(interval.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := loadData(t)
				m := arena(time.Hour)
				m.Spawns = nil
				inst, stop := runMap(t, data, m, nil, options{TickInterval: interval})
				defer stop()
				p := warriorAt(100)
				s := &sink{}
				if err := inst.Join(context.Background(), p, s); err != nil {
					t.Fatal(err)
				}
				time.Sleep(interval)
				synctest.Wait()
				ticks := int(time.Second / interval)
				for seq := range uint32(ticks) {
					_ = inst.Input(p.ID, seq+1, sim.Input{Right: true})
					time.Sleep(interval)
					synctest.Wait()
				}
				if v, _ := s.own(p.ID); math.Abs(v.State.X-300) > 1 {
					t.Fatalf("x = %.1f after a second, want 300", v.State.X)
				}
			})
		})
	}
}

func TestAFloodedInboxCarriesOverToTheNextTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		x, y := townSpawn(t, data)
		inst, cancel := run(t, data, nil, options{})
		defer cancel()
		p := newPlayer("Ayla", x, y)
		s := &sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(tickInterval)
		synctest.Wait()
		for seq := range uint32(1024) { // a tick's whole drain, for nobody
			_ = inst.Input(kernel.NewCharacterID(), seq, sim.Input{})
		}
		_ = inst.Input(p.ID, 1, sim.Input{Right: true})
		var acks []uint32
		for range 2 {
			time.Sleep(tickInterval)
			synctest.Wait()
			acks = append(acks, s.last().AckSeq)
		}
		if acks[0] != 0 || acks[1] != 1 {
			t.Fatalf("acks %v, want [0 1]: the input waits a tick behind the flood", acks)
		}
	})
}

func TestInputQueueAppliesOnePerTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		x, y := townSpawn(t, data)
		inst, cancel := run(t, data, nil, options{})
		defer cancel()
		p := newPlayer("Ayla", x, y)
		s := &sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(25 * tickInterval) // land and settle
		synctest.Wait()

		{ // three inputs arriving in one tick are applied over the next three ticks
			start, _ := s.own(p.ID)
			_ = inst.Input(p.ID, 1, sim.Input{Right: true})
			_ = inst.Input(p.ID, 2, sim.Input{Right: true})
			_ = inst.Input(p.ID, 3, sim.Input{Left: true})
			var acks []uint32
			var xs []float64
			for range 3 {
				time.Sleep(tickInterval)
				synctest.Wait()
				acks = append(acks, s.last().AckSeq)
				v, _ := s.own(p.ID)
				xs = append(xs, v.State.X)
			}
			perTick := 3 * data.Movement().WalkSpeed * data.Movement().Dt
			if acks[0] != 1 || acks[1] != 2 || acks[2] != 3 {
				t.Fatalf("acks %v, want 1 2 3", acks)
			}
			if !near(xs[0], start.State.X+perTick) || !near(xs[1], start.State.X+2*perTick) || !near(xs[2], start.State.X+perTick) {
				t.Fatalf("xs %v from %v: want right, right, left one tick each", xs, start.State.X)
			}
		}

		{ // six queued inputs are cut to the newest two
			for seq := uint32(10); seq <= 15; seq++ {
				_ = inst.Input(p.ID, seq, sim.Input{})
			}
			var acks []uint32
			for range 3 {
				time.Sleep(tickInterval)
				synctest.Wait()
				acks = append(acks, s.last().AckSeq)
			}
			if acks[0] != 14 || acks[1] != 15 || acks[2] != 15 {
				t.Fatalf("acks %v, want 14 15 15: the oldest four were dropped", acks)
			}
		}

		{ // a duplicate sequence number queues once
			_ = inst.Input(p.ID, 20, sim.Input{Right: true})
			_ = inst.Input(p.ID, 20, sim.Input{Left: true})
			_ = inst.Input(p.ID, 21, sim.Input{})
			time.Sleep(2 * tickInterval)
			synctest.Wait()
			if s.last().AckSeq != 21 {
				t.Fatalf("ack %d, want 21: the duplicate did not take a tick of its own", s.last().AckSeq)
			}
		}
	})
}

func TestInputGapHoldsThePlayer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		x, y := townSpawn(t, data)
		inst, cancel := run(t, data, nil, options{})
		defer cancel()
		p := newPlayer("Ayla", x, y)
		s := &sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(25 * tickInterval) // land and settle
		synctest.Wait()
		start, _ := s.own(p.ID)
		state := start.State
		geom := data.Town().Geometry
		params := data.Movement()
		tick := func(in *sim.Input, seq uint32) model.PlayerView {
			if in != nil {
				_ = inst.Input(p.ID, seq, *in)
			}
			time.Sleep(tickInterval)
			synctest.Wait()
			v, _ := s.own(p.ID)
			return v
		}
		right := sim.Input{Right: true}

		{ // a one-tick gap: the player waits, ack_seq repeats, and the next input lands exactly
			for seq := uint32(1); seq <= 3; seq++ {
				tick(&right, seq)
				for range 3 {
					state = sim.Step(state, right, geom, params)
				}
			}
			held := tick(nil, 0)
			if held.State != state || s.last().AckSeq != 3 {
				t.Fatalf("during the gap: state %+v ack %d, want unmoved %+v and ack 3", held.State, s.last().AckSeq, state)
			}
			after := tick(&right, 4)
			for range 3 {
				state = sim.Step(state, right, geom, params)
			}
			if after.State != state || s.last().AckSeq != 4 {
				t.Fatalf("after the gap: state %+v ack %d, want the exact sim state %+v and ack 4", after.State, s.last().AckSeq, state)
			}
		}

		{ // a six-tick gap: five ticks held, then the last keys are simulated
			var moved []bool
			for range 6 {
				v := tick(nil, 0)
				moved = append(moved, v.State != state)
				if s.last().AckSeq != 4 {
					t.Fatalf("ack %d while starved, want 4 repeated", s.last().AckSeq)
				}
			}
			want := []bool{false, false, false, false, false, true}
			for n := range want {
				if moved[n] != want[n] {
					t.Fatalf("moved per starved tick %v, want %v", moved, want)
				}
			}
			for range 3 {
				state = sim.Step(state, right, geom, params)
			}
			if v, _ := s.own(p.ID); v.State != state {
				t.Fatalf("after the hold: %+v, want one tick of the held keys %+v", v.State, state)
			}
			// the next input is applied as usual and the counter starts over
			stop := sim.Input{}
			v := tick(&stop, 5)
			for range 3 {
				state = sim.Step(state, stop, geom, params)
			}
			if v.State != state || s.last().AckSeq != 5 {
				t.Fatalf("after the stall: %+v ack %d, want %+v ack 5", v.State, s.last().AckSeq, state)
			}
			if held := tick(nil, 0); held.State != state {
				t.Fatalf("one tick without input after the stall moved the player: %+v", held.State)
			}
		}
	})
}

func TestServerStateMatchesSimStepExactly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		x, y := townSpawn(t, data)
		inst, cancel := run(t, data, nil, options{})
		defer cancel()
		p := newPlayer("Ayla", x, y)
		s := &sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(25 * tickInterval) // land and settle
		synctest.Wait()
		start, _ := s.own(p.ID)
		state := start.State

		inputs := []sim.Input{
			{Right: true}, {Right: true}, {Right: true, Jump: true}, {Right: true}, {Right: true},
			{}, {}, {Left: true}, {Left: true, Jump: true}, {Left: true}, {Left: true}, {},
			{Right: true}, {Right: true}, {Right: true}, {}, {}, {}, {}, {},
		}
		geom := data.Town().Geometry
		params := data.Movement()
		for n, in := range inputs {
			_ = inst.Input(p.ID, uint32(n+1), in)
			time.Sleep(tickInterval)
			synctest.Wait()
			for range 3 {
				state = sim.Step(state, in, geom, params)
			}
			got, _ := s.own(p.ID)
			if got.State != state { // == like the movement vectors: an exact match
				t.Fatalf("tick %d: server %+v, sim %+v", n+1, got.State, state)
			}
			if s.last().AckSeq != uint32(n+1) {
				t.Fatalf("tick %d: ack %d", n+1, s.last().AckSeq)
			}
		}
	})
}

func TestTickObserver(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		obs := &observer{}
		_, cancel := run(t, data, nil, options{Observer: obs})
		defer cancel()
		time.Sleep(5 * tickInterval)
		synctest.Wait()
		obs.mu.Lock()
		defer obs.mu.Unlock()
		if len(obs.ticks) != 5 {
			t.Fatalf("%d observations in 5 ticks", len(obs.ticks))
		}
	})
}

func TestCheckpoints(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		x, y := townSpawn(t, data)
		cps := &checkpoints{}
		inst, cancel := run(t, data, cps, options{CheckpointInterval: 30 * time.Second})
		defer cancel()
		p := newPlayer("Ayla", x, y)
		p.Exp = 12
		s := &sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(tickInterval)
		synctest.Wait()
		_ = inst.Input(p.ID, 1, sim.Input{Right: true})

		collect := cps.drain

		{ // nothing before the interval
			time.Sleep(29 * time.Second)
			synctest.Wait()
			if got := collect(); len(got) != 0 {
				t.Fatalf("%d checkpoints before 30 s", len(got))
			}
		}
		{ // one checkpoint per player at the interval, with the live state
			time.Sleep(2 * time.Second)
			synctest.Wait()
			got := collect()
			if len(got) != 1 {
				t.Fatalf("%d checkpoints at 30 s, want 1", len(got))
			}
			cp := got[0]
			if cp.ID != p.ID || cp.MapID != "town" || cp.X <= x || cp.Exp != 12 || cp.HP != 204 || cp.MP != 28 || cp.Level != 1 || cp.Stats != p.Stats {
				t.Fatalf("checkpoint %+v", cp)
			}
		}
		{ // and again at the next interval
			time.Sleep(30 * time.Second)
			synctest.Wait()
			if got := collect(); len(got) != 1 {
				t.Fatalf("%d checkpoints at 60 s, want 1", len(got))
			}
		}
		{ // on leave, right away
			_ = inst.LeaveConnection(p.ID, "")
			time.Sleep(tickInterval)
			synctest.Wait()
			got := collect()
			if len(got) != 1 || got[0].ID != p.ID {
				t.Fatalf("checkpoints on leave %+v", got)
			}
		}
		{ // on shutdown, players still on the map
			q := newPlayer("Robin", x, y)
			if err := inst.Join(context.Background(), q, &sink{}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(tickInterval)
			synctest.Wait()
			cancel()
			synctest.Wait()
			got := collect()
			if len(got) != 1 || got[0].ID != q.ID {
				t.Fatalf("checkpoints on shutdown %+v", got)
			}
		}
	})
}

// Players joining together are checkpointed on different ticks, each once an interval.
func TestCheckpointSweepsAreStaggered(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		x, y := townSpawn(t, data)
		cps := &checkpoints{}
		inst, cancel := run(t, data, cps, options{CheckpointInterval: 4 * tickInterval})
		defer cancel()
		drain := func() []kernel.CharacterID {
			var out []kernel.CharacterID
			for _, cp := range cps.drain() {
				out = append(out, cp.ID)
			}
			return out
		}
		saves := map[kernel.CharacterID]int{}
		var joins sync.WaitGroup
		for _, name := range []string{"Ayla", "Robin", "Kai", "Mira"} {
			p := newPlayer(name, x, y)
			saves[p.ID] = 0
			joins.Go(func() {
				if err := inst.Join(context.Background(), p, &sink{}); err != nil {
					t.Error(err)
				}
			})
		}
		joins.Wait()
		synctest.Wait() // the tick that took the joins has finished
		drain()
		for tick := range 8 {
			time.Sleep(tickInterval)
			synctest.Wait()
			got := drain()
			if len(got) != 1 {
				t.Fatalf("tick %d: %d checkpoints, want one player's", tick, len(got))
			}
			saves[got[0]]++
		}
		for id, n := range saves {
			if n != 2 {
				t.Fatalf("%v checkpointed %d times in two intervals", id, n)
			}
		}
	})
}

func TestSlowConsumerIsRemoved(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		x, y := townSpawn(t, data)
		tr := &transferRecorder{}
		inst, cancel := run(t, data, nil, options{Transfer: tr})
		defer cancel()
		ayla, robin := newPlayer("Ayla", x, y), newPlayer("Robin", x, y)
		robin.Connection = "c-robin"
		sa, sr := &sink{}, &sink{}
		if err := inst.Join(context.Background(), ayla, sa); err != nil {
			t.Fatal(err)
		}
		if err := inst.Join(context.Background(), robin, sr); err != nil {
			t.Fatal(err)
		}
		time.Sleep(tickInterval)
		synctest.Wait()
		sr.mu.Lock()
		sr.full = true
		sr.mu.Unlock()
		time.Sleep(2 * tickInterval)
		synctest.Wait()
		found := false
		for _, d := range sa.deltas() {
			for _, id := range d.Removed {
				found = found || id == robin.ID
			}
		}
		if !found {
			t.Fatal("Ayla was never told that Robin (a slow consumer) left")
		}
		tr.mu.Lock()
		dropped := slices.Clone(tr.dropped)
		tr.mu.Unlock()
		if len(dropped) != 1 || dropped[0] != robin.ID.String()+" c-robin" {
			t.Fatalf("the world heard of drops %v, want Robin's through c-robin, so it forgets the presence", dropped)
		}
		if err := inst.Join(context.Background(), robin, &sink{full: true}); err == nil {
			t.Fatal("a join whose sink refuses the snapshot succeeded")
		}
	})
}

func TestReplayMatchesSim(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		x, y := townSpawn(t, data)
		inst, cancel := run(t, data, nil, options{})
		defer cancel()
		p := newPlayer("Ayla", x, y)
		s := &sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(tickInterval)
		synctest.Wait()
		snap := s.all()[0].(model.Snapshot)
		state := snap.Players[0].State
		for range 3 { // the join tick itself ran 3 sub-steps with no input
			state = sim.Step(state, sim.Input{}, data.Town().Geometry, data.Movement())
		}
		recorded := []sim.Input{
			{Right: true}, {Right: true}, {Right: true, Jump: true}, {Right: true}, {Right: true}, {}, {}, {Left: true}, {Left: true, Jump: true}, {Left: true},
			{}, {}, {}, {}, {}, {Up: true}, {}, {Right: true}, {Right: true}, {},
		}
		for i, in := range recorded {
			_ = inst.Input(p.ID, uint32(i+1), in) //nolint:gosec // small test index
			time.Sleep(tickInterval)
			synctest.Wait()
			for range 3 {
				state = sim.Step(state, in, data.Town().Geometry, data.Movement())
			}
			got, ok := s.own(p.ID)
			if !ok {
				t.Fatalf("tick %d: no view", i+1)
			}
			if got.State != state {
				t.Fatalf("tick %d: instance %+v, sim %+v", i+1, got.State, state)
			}
			if s.last().AckSeq != uint32(i+1) { //nolint:gosec // small test index
				t.Fatalf("tick %d: ack %d", i+1, s.last().AckSeq)
			}
		}
	})
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// walk sends the same Input once per tick for n ticks after seq, returning the last seq used.
func walk(inst *application.Instance, id kernel.CharacterID, seq uint32, in sim.Input, ticks int) uint32 {
	for range ticks {
		seq++
		_ = inst.Input(id, seq, in)
		time.Sleep(tickInterval)
		synctest.Wait()
	}
	return seq
}

// A Leave and a rejoin in one tick leave the player on everyone's screen: the Delta lists them, not as removed.
func TestALeaveAndARejoinInOneTickKeepThePlayerShown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		x, y := townSpawn(t, data)
		inst, cancel := run(t, data, nil, options{})
		defer cancel()
		ayla, robin := newPlayer("Ayla", x, y), newPlayer("Robin", x+100, y)
		ayla.Connection = "c-1"
		join(t, inst, ayla)
		sr := join(t, inst, robin)
		before := len(sr.all())
		_ = inst.LeaveConnection(ayla.ID, "c-1")
		ayla.Connection = "c-2"
		joined := make(chan error, 1)
		go func() { joined <- inst.Join(context.Background(), ayla, &sink{}) }()
		synctest.Wait()
		time.Sleep(tickInterval)
		synctest.Wait()
		if err := <-joined; err != nil {
			t.Fatal(err)
		}
		shown := false
		for _, e := range sr.all()[before:] {
			d, ok := e.(model.Delta)
			if !ok {
				continue
			}
			if slices.Contains(d.Removed, ayla.ID) {
				t.Fatalf("Robin was told Ayla left: %+v", d)
			}
			shown = shown || slices.ContainsFunc(d.Players, func(v model.PlayerView) bool { return v.ID == ayla.ID })
		}
		if !shown {
			t.Fatal("Robin was not shown Ayla after the rejoin")
		}
	})
}
