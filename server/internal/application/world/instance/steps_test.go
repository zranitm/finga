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
