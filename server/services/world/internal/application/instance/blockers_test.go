package instance_test

import (
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/sim"
	"finga/services/world/worldtest"
)

// C12: a player walking into a mob stops at its side, as the frame of the input's view showed it. The view is the client's, so it's held to the frames kept: a made-up old or future one blocks as the oldest kept or the last sent, and neither lets the player through. A starved tick walks on with the held input's view, held again.
func TestAPlayerStopsAtAMobsSide(t *testing.T) {
	for _, tc := range []struct {
		name string
		view uint32
	}{{"no view yet", 0}, {"a view long gone", 1}, {"a view not yet sent", 1 << 30}} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, worldtest.LoadData(t), arena(time.Hour), nil, worldtest.Options{})
				defer stop()
				time.Sleep(worldtest.TickInterval)
				synctest.Wait()
				s := join(t, inst, warriorAt(200))
				id := idOf(t, s)
				_ = inst.Input(id, 1, sim.Input{Right: true}, tc.view)
				time.Sleep(25 * worldtest.TickInterval) // held still, then the held input walks on: 120 px, past the slime on its platform at 284-316
				synctest.Wait()
				if v, _ := s.Own(id); v.State.X != 272 || v.State.VX != 0 {
					t.Fatalf("the player stood at %+v; want stopped at x 272, the slime's left side", v.State)
				}
			})
		})
	}
}
