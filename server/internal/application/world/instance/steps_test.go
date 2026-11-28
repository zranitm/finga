package instance_test

import (
	"fmt"
	"strings"
	"testing"
	"testing/synctest"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/quant"
	"finga/internal/shared/sim"
)

// A player walking inside a cell is a step to the others watching it, whole units from its last place, and a view the client reads back to within a centimetre of where it stands; the walker's own frame carries it whole, exact.
func TestAWalkingPlayerGoesOutAsStepsAndItsOwnViewWhole(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), plain(t, nil, nil), nil, worldtest.Options{})
		defer stop()
		walker, watcher := worldtest.NewPlayer("Walker", 100, 100), worldtest.NewPlayer("Watcher", 104, 100)
		ws, wt := join(t, inst, walker), join(t, inst, watcher)
		tick(2)
		var seq uint32
		for range 20 {
			seq++
			_ = inst.Input(walker.ID, seq, sim.Input{Right: true}, 0)
			tick(1)
		}
		steps, whole := 0, 0
		for _, d := range wt.Deltas() {
			for _, v := range d.Sent.Players {
				if v.ID == walker.ID {
					whole++
				}
			}
			steps += len(d.Sent.Steps)
		}
		if steps < 10 || whole > 3 {
			t.Fatalf("%d steps and %d whole views of the walker in 20 ticks of walking; want steps, the whole view for the first frames only", steps, whole)
		}
		own, _ := ws.Own(walker.ID)
		seen := sightOf(t, wt).players[walker.ID]
		if seen.State.X != own.State.X || seen.State.Z != own.State.Z {
			t.Fatalf("the watcher holds the walker at %v, %v; it stands at %v, %v", seen.State.X, seen.State.Z, own.State.X, own.State.Z)
		}
		if own.State.X == quant.Metres(quant.Position(own.State.X)) {
			t.Fatalf("the walker's own x, %v, is a whole centimetre; want the exact one the sim has", own.State.X)
		}
	})
}

// A player who comes to another cell is a step to those who held it in the cell it left, and whole to those who see its new cell and not the old, once: it isn't sent whole to everyone who sees the cell.
func TestAPlayerComingToACellIsWholeOnlyToThoseWhoDidNotHoldIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cell := data.Interest().Cell
		inst, stop := worldtest.RunMap(t, data, plain(t, nil, nil), nil, worldtest.Options{})
		defer stop()
		walker := worldtest.NewPlayer("Walker", cell-2, 100)
		holder := worldtest.NewPlayer("Holder", cell-6, 100)                                     // sees the cell it leaves and the one it comes to
		newcomer := worldtest.NewPlayer("Newcomer", cell*float64(data.Interest().Side+1)+4, 100) // sees the cell it comes to, not the one it leaves
		ws, hs, ns := join(t, inst, walker), join(t, inst, holder), join(t, inst, newcomer)
		tick(2)
		if _, held := sightOf(t, ns).players[walker.ID]; held {
			t.Fatal("the newcomer holds the walker before it comes to its cells")
		}
		before := map[*worldtest.Sink]int{hs: len(hs.Deltas()), ns: len(ns.Deltas())}
		var seq uint32
		for {
			if own, _ := ws.Own(walker.ID); own.State.X > cell+2 {
				break
			}
			seq = worldtest.Walk(inst, walker.ID, seq, sim.Input{Right: true}, 1)
		}
		tick(2)
		count := func(s *worldtest.Sink) (whole, steps int) {
			for _, d := range s.Deltas()[before[s]:] {
				for _, v := range d.Sent.Players {
					if v.ID == walker.ID {
						whole++
					}
				}
				steps += len(d.Sent.Steps)
			}
			return whole, steps
		}
		if whole, steps := count(hs); whole != 0 || steps == 0 {
			t.Fatalf("the holder was sent the walker whole %d times and %d steps; want its steps alone", whole, steps)
		}
		if whole, _ := count(ns); whole != 1 {
			t.Fatalf("the newcomer was sent the walker whole %d times; want once, as it came to the cell", whole)
		}
		own, _ := ws.Own(walker.ID)
		for name, s := range map[string]*worldtest.Sink{"holder": hs, "newcomer": ns} {
			seen := sightOf(t, s).players[walker.ID]
			if seen.State.X != own.State.X || seen.State.Z != own.State.Z {
				t.Fatalf("the %s holds the walker at %v, %v; it stands at %v, %v", name, seen.State.X, seen.State.Z, own.State.X, own.State.Z)
			}
		}
	})
}

// crowdedData is the data with a crowd at 3 players in a cell and the eight round it.
func crowdedData(t *testing.T) *gamedata.Data {
	return worldtest.LoadDataWith(t, map[string]func(string) string{"world.yaml": func(s string) string { return strings.Replace(s, "crowd: 60", "crowd: 3", 1) }})
}

// walkOn has p press in for ticks, and returns the ticks in the last last of them that watcher was sent a step in.
func walkOn(inst *instance.Instance, p model.Player, watcher *worldtest.Sink, seq *uint32, in sim.Input, ticks, last int) int {
	steps := 0
	for k := range ticks {
		before := len(watcher.Deltas())
		*seq = worldtest.Walk(inst, p.ID, *seq, in, 1)
		if k >= ticks-last {
			for _, d := range watcher.Deltas()[before:] {
				if len(d.Sent.Steps) > 0 {
					steps++
				}
			}
		}
	}
	return steps
}

// In a crowd a player's steps of motion alone go every second tick, a step of anything more goes at once, and a quiet place keeps every tick: the watcher holds the walker where it stands within a tick's walk, and where it stopped once it has.
func TestACrowdsStepsOfMotionAloneGoEverySecondTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := crowdedData(t)
		inst, stop := worldtest.RunMap(t, data, plain(t, nil, nil), nil, worldtest.Options{})
		defer stop()
		walker := worldtest.NewPlayer("Walker", 20, 100)
		sinks := []*worldtest.Sink{join(t, inst, walker)}
		for k := range 3 { // 4 in the cell: past the crowd of 3
			sinks = append(sinks, join(t, inst, worldtest.NewPlayer(fmt.Sprintf("Crowd%d", k), 22+float64(k), 100)))
		}
		tick(2)
		watcher := sinks[1]
		var seq uint32
		if rising := walkOn(inst, walker, watcher, &seq, sim.Input{Right: true}, 20, 20); rising < 20 { // the sprint's count changes every tick of its first forty: steps of more than motion
			t.Fatalf("%d of 20 ticks of a walk's start sent a step; want every one, as each says the sprint's count", rising)
		}
		slow := sim.Input{Right: true, Walk: true} // walking pace has no sprint to count: its steps say motion alone
		if steady := walkOn(inst, walker, watcher, &seq, slow, 60, 20); steady < 9 || steady > 11 {
			t.Fatalf("%d of 20 ticks of a crowd's walk sent a step; want every second", steady)
		}
		for range 2 { // the stop is held back on one parity of the tick it falls on, then the other
			seq = worldtest.Walk(inst, walker.ID, seq, sim.Input{Walk: true}, 3)
			tick(2)
			own, _ := sinks[0].Own(walker.ID)
			if seen := sightOf(t, watcher).players[walker.ID]; seen.State.X != own.State.X || seen.State.Z != own.State.Z || seen.State.VX != own.State.VX {
				t.Fatalf("the watcher holds the walker at (%v, %v) moving %v after it stopped; it stands at (%v, %v) moving %v", seen.State.X, seen.State.Z, seen.State.VX, own.State.X, own.State.Z, own.State.VX)
			}
			walkOn(inst, walker, watcher, &seq, slow, 60, 0)
		}
	})
}

func TestAQuietPlacesStepsOfMotionAloneGoEveryTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := crowdedData(t)
		inst, stop := worldtest.RunMap(t, data, plain(t, nil, nil), nil, worldtest.Options{})
		defer stop()
		walker := worldtest.NewPlayer("Walker", 20, 100)
		sinks := []*worldtest.Sink{join(t, inst, walker), join(t, inst, worldtest.NewPlayer("Other", 22, 100))} // 2 in the cell: under the crowd of 3
		tick(2)
		var seq uint32
		if steady := walkOn(inst, walker, sinks[1], &seq, sim.Input{Right: true}, 80, 20); steady < 20 {
			t.Fatalf("%d of 20 ticks of a quiet place's walk sent a step; want every one", steady)
		}
	})
}

// A player who joins a crowd is shown a walker whose step waits as its watchers hold it, so the watchers' next step still reads from the joiner's view: none of them goes whole.
func TestAJoinIsShownAWaitingWalkerAsItsWatchersHoldIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := crowdedData(t)
		inst, stop := worldtest.RunMap(t, data, plain(t, nil, nil), nil, worldtest.Options{})
		defer stop()
		walker := worldtest.NewPlayer("Walker", 20, 100)
		ws := join(t, inst, walker)
		var watchers []*worldtest.Sink
		for k := range 3 {
			watchers = append(watchers, join(t, inst, worldtest.NewPlayer(fmt.Sprintf("Crowd%d", k), 22+float64(k), 100)))
		}
		var seq uint32
		walkOn(inst, walker, watchers[0], &seq, sim.Input{Right: true}, 60, 0)
		for range 6 { // a joiner on each of both parities of the tick the walker is held on
			before := len(watchers[0].Deltas())
			joiner := worldtest.NewPlayer("Joiner", 30, 100)
			js := join(t, inst, joiner)
			seq = worldtest.Walk(inst, walker.ID, seq, sim.Input{Right: true}, 6)
			for _, d := range watchers[0].Deltas()[before:] {
				for _, v := range d.Sent.Players {
					if v.ID == walker.ID {
						t.Fatalf("tick %d: the walker was sent whole to a watcher after a join; want its steps from the baseline the join was shown", d.Tick)
					}
				}
			}
			own, _ := ws.Own(walker.ID)
			if seen := sightOf(t, js).players[walker.ID]; seen.State.X > own.State.X || own.State.X-seen.State.X > 1 {
				t.Fatalf("the joiner holds the walker at x %v; it stands at %v", seen.State.X, own.State.X)
			}
			if err := inst.Leave(joiner.ID, "", nil); err != nil {
				t.Fatal(err)
			}
			tick(1)
		}
	})
}
