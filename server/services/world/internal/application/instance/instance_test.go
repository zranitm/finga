package instance_test

import (
	"context"
	"math"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// onStream is a player's sink whose frames go to a stream shared with others, as a gateway's.
type onStream struct {
	*worldtest.Sink
	stream *frames
}

func (s onStream) Stream() instance.Stream { return s.stream }

// frames records each frame's recipients.
type frames struct {
	mu   sync.Mutex
	sent [][]model.Recipient
}

func (f *frames) SendFrame(_ *model.Frame, to []model.Recipient) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, slices.Clone(to))
	return true
}

// A tick's frame goes once to each stream, naming each of its players with the last input applied for it.
func TestATicksFrameGoesOnceToEachStream(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		inst, stop := worldtest.RunMap(t, data, data.Town(), nil, worldtest.Options{})
		defer stop()
		gateway := &frames{}
		ayla, robin, bruno := worldtest.NewPlayer("Ayla", x, y), worldtest.NewPlayer("Robin", x, y), worldtest.NewPlayer("Bruno", x, y)
		ayla.Connection, robin.Connection = "c-ayla", "c-robin"
		for _, j := range []struct {
			p model.Player
			s instance.Sink
		}{{ayla, onStream{&worldtest.Sink{}, gateway}}, {robin, onStream{&worldtest.Sink{}, gateway}}, {bruno, &worldtest.Sink{}}} {
			if err := inst.Join(context.Background(), j.p, j.s); err != nil {
				t.Fatal(err)
			}
		}
		if err := inst.Input(ayla.ID, 7, sim.Input{}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		gateway.mu.Lock()
		defer gateway.mu.Unlock()
		want := []model.Recipient{{Character: ayla.ID, Connection: "c-ayla", AckSeq: 7}, {Character: robin.ID, Connection: "c-robin"}}
		if len(gateway.sent) == 0 || !slices.Equal(gateway.sent[len(gateway.sent)-1], want) {
			t.Fatalf("frames to the shared stream %+v, want each tick's once to %+v", gateway.sent, want)
		}
	})
}

func TestJoinSnapshotAndDeltas(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		inst, cancel := worldtest.Run(t, data, nil, worldtest.Options{})
		defer cancel()

		ayla, robin := worldtest.NewPlayer("Ayla", x, y), worldtest.NewPlayer("Robin", x+100, y)
		sa, sr := &worldtest.Sink{}, &worldtest.Sink{}
		if err := inst.Join(context.Background(), ayla, sa); err != nil {
			t.Fatal(err)
		}
		time.Sleep(worldtest.TickInterval) // the join is handled on the next tick
		synctest.Wait()

		{ // a joining player gets a Snapshot first, then Deltas
			events := sa.All()
			if len(events) == 0 {
				t.Fatal("no events")
			}
			snap, ok := events[0].(model.Snapshot)
			if !ok || snap.MapID != "town" || len(snap.Players) != 1 || snap.Players[0].ID != ayla.ID || snap.Players[0].State.X != x {
				t.Fatalf("first event %+v", events[0])
			}
			if _, ok := events[1].(worldtest.Delta); !ok {
				t.Fatalf("second event %T", events[1])
			}
		}

		{ // deltas arrive every tick
			before := len(sa.Deltas())
			time.Sleep(10 * worldtest.TickInterval)
			synctest.Wait()
			if got := len(sa.Deltas()) - before; got != 10 {
				t.Fatalf("%d deltas in 10 ticks", got)
			}
		}

		{ // others see the join
			if err := inst.Join(context.Background(), robin, sr); err != nil {
				t.Fatal(err)
			}
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			found := false
			for _, d := range sa.Deltas() {
				for _, p := range d.Players {
					found = found || (p.ID == robin.ID && p.Name == "Robin")
				}
			}
			if !found {
				t.Fatalf("no delta of Ayla's carries Robin: %+v", sa.Last())
			}
			snap := sr.All()[0].(model.Snapshot)
			if len(snap.Players) != 2 {
				t.Fatalf("Robin's snapshot has %d players", len(snap.Players))
			}
		}

		{ // others see the leave
			_ = inst.Leave(robin.ID, "", nil)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			d := sa.Last()
			if len(d.Removed) != 1 || d.Removed[0] != robin.ID {
				t.Fatalf("removed %v", d.Removed)
			}
			n := len(sr.All())
			time.Sleep(3 * worldtest.TickInterval)
			synctest.Wait()
			if len(sr.All()) != n {
				t.Fatal("Robin kept receiving events after leaving")
			}
		}

		{ // a player standing still is not in the worldtest.Delta
			time.Sleep(20 * worldtest.TickInterval) // long enough to land and settle
			synctest.Wait()
			if d := sa.Last(); len(d.Players) != 0 {
				t.Fatalf("idle delta carries %d players", len(d.Players))
			}
		}
	})
}

func TestInputsApplyInSequenceOrder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		inst, cancel := worldtest.Run(t, data, nil, worldtest.Options{})
		defer cancel()
		p := worldtest.NewPlayer("Ayla", x, y)
		s := &worldtest.Sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(25 * worldtest.TickInterval) // land and settle
		synctest.Wait()
		startX, _ := s.Own(p.ID)
		if !startX.State.OnGround {
			t.Fatalf("not on the ground: %+v", startX.State)
		}

		{ // one input per tick moves at walk speed
			_ = inst.Input(p.ID, 1, sim.Input{Right: true})
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			v, _ := s.Own(p.ID)
			want := x + 3*data.Movement().WalkSpeed*data.Movement().Dt
			if !near(v.State.X, want) {
				t.Fatalf("x = %v, want %v after one tick of walking", v.State.X, want)
			}
			if s.Last().AckSeq != 1 {
				t.Fatalf("ack_seq %d, want 1", s.Last().AckSeq)
			}
		}

		{ // two inputs in one tick apply over two ticks, in sequence order
			before, _ := s.Own(p.ID)
			_ = inst.Input(p.ID, 3, sim.Input{})            // stop
			_ = inst.Input(p.ID, 2, sim.Input{Right: true}) // arrives out of order but is older
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			after, _ := s.Own(p.ID)
			want := before.State.X + 3*data.Movement().WalkSpeed*data.Movement().Dt
			if !near(after.State.X, want) || s.Last().AckSeq != 2 {
				t.Fatalf("x = %v ack %d, want %v and ack 2: the older input applies first", after.State.X, s.Last().AckSeq, want)
			}
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			stopped, _ := s.Own(p.ID)
			if stopped.State.X != after.State.X || s.Last().AckSeq != 3 {
				t.Fatalf("x %v -> %v ack %d, want a stop with ack 3 on the next tick", after.State.X, stopped.State.X, s.Last().AckSeq)
			}
		}

		{ // old and duplicate sequence numbers are ignored
			before, _ := s.Own(p.ID)
			_ = inst.Input(p.ID, 2, sim.Input{Right: true}) // old
			_ = inst.Input(p.ID, 3, sim.Input{Left: true})  // duplicate seq
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			after, _ := s.Own(p.ID)
			if after.State.X != before.State.X || s.Last().AckSeq != 3 {
				t.Fatalf("state %+v ack %d; old inputs were applied", after.State, s.Last().AckSeq)
			}
		}

		{ // one input moves one tick; with none queued the player waits
			before, _ := s.Own(p.ID)
			_ = inst.Input(p.ID, 4, sim.Input{Left: true})
			time.Sleep(3 * worldtest.TickInterval)
			synctest.Wait()
			after, _ := s.Own(p.ID)
			want := before.State.X - 3*data.Movement().WalkSpeed*data.Movement().Dt
			if !near(after.State.X, want) {
				t.Fatalf("x = %v, want %v after one tick of walking left and two of waiting", after.State.X, want)
			}
		}

		{ // inputs for an unknown player are ignored
			if inst.Input(kernel.NewCharacterID(), 1, sim.Input{Right: true}) != nil {
				t.Fatal("the inbox refused")
			}
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
		}
	})
}

// A player walks 200 px a second whatever the tick rate, as the client predicts; a new warrior's 7 AGI make that 1.01 times as far in a hunting map.
func TestWalkingSpeedDoesNotDependOnTheTickRate(t *testing.T) {
	for _, interval := range []time.Duration{50 * time.Millisecond, 100 * time.Millisecond} {
		t.Run(interval.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				m := arena(time.Hour)
				m.Spawns = nil
				inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{TickInterval: interval})
				defer stop()
				p := warriorAt(100)
				s := &worldtest.Sink{}
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
				if v, _ := s.Own(p.ID); math.Abs(v.State.X-302) > 0.01 {
					t.Fatalf("x = %.2f after a second, want 302", v.State.X)
				}
			})
		})
	}
}

func TestAFloodedInboxCarriesOverToTheNextTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		inst, cancel := worldtest.Run(t, data, nil, worldtest.Options{})
		defer cancel()
		p := worldtest.NewPlayer("Ayla", x, y)
		s := &worldtest.Sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		for seq := range uint32(1024) { // a tick's whole drain, for nobody
			_ = inst.Input(kernel.NewCharacterID(), seq, sim.Input{})
		}
		_ = inst.Input(p.ID, 1, sim.Input{Right: true})
		var acks []uint32
		for range 2 {
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			acks = append(acks, s.Last().AckSeq)
		}
		if acks[0] != 0 || acks[1] != 1 {
			t.Fatalf("acks %v, want [0 1]: the input waits a tick behind the flood", acks)
		}
	})
}

func TestInputQueueAppliesOnePerTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		inst, cancel := worldtest.Run(t, data, nil, worldtest.Options{})
		defer cancel()
		p := worldtest.NewPlayer("Ayla", x, y)
		s := &worldtest.Sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(25 * worldtest.TickInterval) // land and settle
		synctest.Wait()

		{ // three inputs arriving in one tick are applied over the next three ticks
			start, _ := s.Own(p.ID)
			_ = inst.Input(p.ID, 1, sim.Input{Right: true})
			_ = inst.Input(p.ID, 2, sim.Input{Right: true})
			_ = inst.Input(p.ID, 3, sim.Input{Left: true})
			var acks []uint32
			var xs []float64
			for range 3 {
				time.Sleep(worldtest.TickInterval)
				synctest.Wait()
				acks = append(acks, s.Last().AckSeq)
				v, _ := s.Own(p.ID)
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
				time.Sleep(worldtest.TickInterval)
				synctest.Wait()
				acks = append(acks, s.Last().AckSeq)
			}
			if acks[0] != 14 || acks[1] != 15 || acks[2] != 15 {
				t.Fatalf("acks %v, want 14 15 15: the oldest four were dropped", acks)
			}
		}

		{ // a duplicate sequence number queues once
			_ = inst.Input(p.ID, 20, sim.Input{Right: true})
			_ = inst.Input(p.ID, 20, sim.Input{Left: true})
			_ = inst.Input(p.ID, 21, sim.Input{})
			time.Sleep(2 * worldtest.TickInterval)
			synctest.Wait()
			if s.Last().AckSeq != 21 {
				t.Fatalf("ack %d, want 21: the duplicate did not take a tick of its own", s.Last().AckSeq)
			}
		}
	})
}

// The hold and the backlog are spans of time, so at 60 Hz a 200 ms gap still holds the player, as at 20 Hz.
func TestTheInputHoldLastsAsLongAtAnyRate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		interval := time.Second / 60
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		inst, cancel := worldtest.Run(t, data, nil, worldtest.Options{TickInterval: interval})
		defer cancel()
		p := worldtest.NewPlayer("Ayla", x, y)
		s := &worldtest.Sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(75 * interval) // land and settle
		synctest.Wait()
		_ = inst.Input(p.ID, 1, sim.Input{Right: true})
		time.Sleep(interval)
		synctest.Wait()
		before, _ := s.Own(p.ID)
		for n := range 12 {
			time.Sleep(interval)
			synctest.Wait()
			if v, _ := s.Own(p.ID); v.State != before.State {
				t.Fatalf("starved tick %d moved the player; a 200 ms gap holds them", n+1)
			}
		}
	})
}

func TestInputGapHoldsThePlayer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		inst, cancel := worldtest.Run(t, data, nil, worldtest.Options{})
		defer cancel()
		p := worldtest.NewPlayer("Ayla", x, y)
		s := &worldtest.Sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(25 * worldtest.TickInterval) // land and settle
		synctest.Wait()
		start, _ := s.Own(p.ID)
		state := start.State
		geom := data.Town().Geometry
		params := data.Movement()
		tick := func(in *sim.Input, seq uint32) model.PlayerView {
			if in != nil {
				_ = inst.Input(p.ID, seq, *in)
			}
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			v, _ := s.Own(p.ID)
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
			if held.State != state || s.Last().AckSeq != 3 {
				t.Fatalf("during the gap: state %+v ack %d, want unmoved %+v and ack 3", held.State, s.Last().AckSeq, state)
			}
			after := tick(&right, 4)
			for range 3 {
				state = sim.Step(state, right, geom, params)
			}
			if after.State != state || s.Last().AckSeq != 4 {
				t.Fatalf("after the gap: state %+v ack %d, want the exact sim state %+v and ack 4", after.State, s.Last().AckSeq, state)
			}
		}

		{ // a six-tick gap: five ticks held, then the last keys are simulated
			var moved []bool
			for range 6 {
				v := tick(nil, 0)
				moved = append(moved, v.State != state)
				if s.Last().AckSeq != 4 {
					t.Fatalf("ack %d while starved, want 4 repeated", s.Last().AckSeq)
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
			if v, _ := s.Own(p.ID); v.State != state {
				t.Fatalf("after the hold: %+v, want one tick of the held keys %+v", v.State, state)
			}
			// the next input is applied as usual and the counter starts over
			stop := sim.Input{}
			v := tick(&stop, 5)
			for range 3 {
				state = sim.Step(state, stop, geom, params)
			}
			if v.State != state || s.Last().AckSeq != 5 {
				t.Fatalf("after the stall: %+v ack %d, want %+v ack 5", v.State, s.Last().AckSeq, state)
			}
			if held := tick(nil, 0); held.State != state {
				t.Fatalf("one tick without input after the stall moved the player: %+v", held.State)
			}
		}
	})
}

func TestServerStateMatchesSimStepExactly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		inst, cancel := worldtest.Run(t, data, nil, worldtest.Options{})
		defer cancel()
		p := worldtest.NewPlayer("Ayla", x, y)
		s := &worldtest.Sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(25 * worldtest.TickInterval) // land and settle
		synctest.Wait()
		start, _ := s.Own(p.ID)
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
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			for range 3 {
				state = sim.Step(state, in, geom, params)
			}
			got, _ := s.Own(p.ID)
			if got.State != state { // == like the movement vectors: an exact match
				t.Fatalf("tick %d: server %+v, sim %+v", n+1, got.State, state)
			}
			if s.Last().AckSeq != uint32(n+1) {
				t.Fatalf("tick %d: ack %d", n+1, s.Last().AckSeq)
			}
		}
	})
}

func TestTickObserver(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		obs := &worldtest.Observer{}
		_, cancel := worldtest.Run(t, data, nil, worldtest.Options{Observer: obs})
		defer cancel()
		time.Sleep(5 * worldtest.TickInterval)
		synctest.Wait()
		if n := obs.Ticks(); n != 5 {
			t.Fatalf("%d observations in 5 ticks", n)
		}
	})
}

func TestCheckpoints(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		cps := &worldtest.Checkpoints{}
		inst, cancel := worldtest.Run(t, data, cps, worldtest.Options{CheckpointInterval: 30 * time.Second})
		defer cancel()
		p := worldtest.NewPlayer("Ayla", x, y)
		p.Exp = 12
		s := &worldtest.Sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		_ = inst.Input(p.ID, 1, sim.Input{Right: true})

		collect := cps.Drain

		{ // nothing before the interval
			time.Sleep(29 * time.Second)
			synctest.Wait()
			if got := collect(); len(got) != 0 {
				t.Fatalf("%d checkpoints before 30 s", len(got))
			}
		}
		{ // one checkpoint per player at the interval, with the live state: MP regenerated from 28 at 0.35 a second, 10 in 30 seconds
			time.Sleep(2 * time.Second)
			synctest.Wait()
			got := collect()
			if len(got) != 1 {
				t.Fatalf("%d checkpoints at 30 s, want 1", len(got))
			}
			cp := got[0]
			if cp.ID != p.ID || cp.MapID != "town" || cp.X <= x || cp.Exp != 12 || cp.HP != 144 || cp.MP != 38 || cp.Level != 1 || cp.Points != p.Points {
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
			_ = inst.Leave(p.ID, "", nil)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			got := collect()
			if len(got) != 1 || got[0].ID != p.ID {
				t.Fatalf("checkpoints on leave %+v", got)
			}
		}
		{ // on shutdown, players still on the map
			q := worldtest.NewPlayer("Robin", x, y)
			if err := inst.Join(context.Background(), q, &worldtest.Sink{}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(worldtest.TickInterval)
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
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		cps := &worldtest.Checkpoints{}
		inst, cancel := worldtest.Run(t, data, cps, worldtest.Options{CheckpointInterval: 4 * worldtest.TickInterval})
		defer cancel()
		drain := func() []kernel.CharacterID {
			var out []kernel.CharacterID
			for _, cp := range cps.Drain() {
				out = append(out, cp.ID)
			}
			return out
		}
		saves := map[kernel.CharacterID]int{}
		var joins sync.WaitGroup
		for _, name := range []string{"Ayla", "Robin", "Kai", "Mira"} {
			p := worldtest.NewPlayer(name, x, y)
			saves[p.ID] = 0
			joins.Go(func() {
				if err := inst.Join(context.Background(), p, &worldtest.Sink{}); err != nil {
					t.Error(err)
				}
			})
		}
		joins.Wait()
		synctest.Wait() // the tick that took the joins has finished
		drain()
		for tick := range 8 {
			time.Sleep(worldtest.TickInterval)
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
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		tr := &transferRecorder{}
		inst, cancel := worldtest.Run(t, data, nil, worldtest.Options{Transfer: tr})
		defer cancel()
		ayla, robin := worldtest.NewPlayer("Ayla", x, y), worldtest.NewPlayer("Robin", x, y)
		robin.Connection = "c-robin"
		sa, sr := &worldtest.Sink{}, &worldtest.Sink{}
		if err := inst.Join(context.Background(), ayla, sa); err != nil {
			t.Fatal(err)
		}
		if err := inst.Join(context.Background(), robin, sr); err != nil {
			t.Fatal(err)
		}
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		sr.Refuse()
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		found := false
		for _, d := range sa.Deltas() {
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
		if err := inst.Join(context.Background(), robin, worldtest.Refusing()); err == nil {
			t.Fatal("a join whose sink refuses the snapshot succeeded")
		}
	})
}

func TestReplayMatchesSim(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		inst, cancel := worldtest.Run(t, data, nil, worldtest.Options{})
		defer cancel()
		p := worldtest.NewPlayer("Ayla", x, y)
		s := &worldtest.Sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		snap := s.All()[0].(model.Snapshot)
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
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			for range 3 {
				state = sim.Step(state, in, data.Town().Geometry, data.Movement())
			}
			got, ok := s.Own(p.ID)
			if !ok {
				t.Fatalf("tick %d: no view", i+1)
			}
			if got.State != state {
				t.Fatalf("tick %d: instance %+v, sim %+v", i+1, got.State, state)
			}
			if s.Last().AckSeq != uint32(i+1) { //nolint:gosec // small test index
				t.Fatalf("tick %d: ack %d", i+1, s.Last().AckSeq)
			}
		}
	})
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// A Leave and a rejoin in one tick leave the player on everyone's screen: the Delta lists them, not as removed.
func TestALeaveAndARejoinInOneTickKeepThePlayerShown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		x, y := worldtest.TownSpawn(t, data)
		inst, cancel := worldtest.Run(t, data, nil, worldtest.Options{})
		defer cancel()
		ayla, robin := worldtest.NewPlayer("Ayla", x, y), worldtest.NewPlayer("Robin", x+100, y)
		ayla.Connection = "c-1"
		join(t, inst, ayla)
		sr := join(t, inst, robin)
		before := len(sr.All())
		_ = inst.Leave(ayla.ID, "c-1", nil)
		ayla.Connection = "c-2"
		joined := make(chan error, 1)
		go func() { joined <- inst.Join(context.Background(), ayla, &worldtest.Sink{}) }()
		synctest.Wait()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		if err := <-joined; err != nil {
			t.Fatal(err)
		}
		shown := false
		for _, e := range sr.All()[before:] {
			d, ok := e.(worldtest.Delta)
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
