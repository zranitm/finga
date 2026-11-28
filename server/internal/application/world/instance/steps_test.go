package instance_test

import (
	"testing"
	"testing/synctest"

	"finga/internal/application/world/worldtest"
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
