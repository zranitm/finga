package instance_test

import (
	"context"
	"math"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/modules/world/internal/model"
	"finga/internal/modules/world/worldtest"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/sim"
)

// cornered is the arena with a slime at the player's south side and, at their north side, the wall from z 22 or the map's edge.
func cornered(t *testing.T, wall bool) (*gamedata.Map, float64) {
	t.Helper()
	m := arena(t, time.Hour)
	z := 40 - 0.375 // touching the map's north edge
	if wall {
		m.Geometry.Boxes = append(m.Geometry.Boxes, sim.Box{X: 20, Z: 23, HalfW: 20, HalfD: 1, UX: 1, Top: 4})
		z = 22 - 0.375
	}
	m.Spawns[0].At, m.Spawns[0].Radius = gamedata.Point{X: arenaX, Z: z - touching}, 1e-6
	return worldtest.Gridded(t, m), z
}

// Knockback into a wall stops at its face, and the hop lands the player back where they stood.
func TestKnockbackStopsAtAWall(t *testing.T) {
	for _, c := range []struct {
		name string
		wall bool
	}{{"north into a box's face", true}, {"north into the map's edge", false}} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				m, z := cornered(t, c.wall)
				inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := warriorAt(z)
				s := join(t, inst, p)
				mobHitsOn(t, s, p.ID, 1)
				tick(15) // the knockback's hop and the landing, before the slime's next hop
				if got := damages(s); len(got) != 1 || got[0].Target.Player != p.ID {
					t.Fatalf("damages %+v, want the slime's one hit", got)
				}
				for _, d := range s.Deltas() {
					for _, v := range d.Players {
						if !m.Holds(v.State.X, v.State.Y, v.State.Z, data.Movement().Step) || v.State.Z > z+1e-9 {
							t.Fatalf("tick %d: the player stands at %+v, past the wall's face", d.Tick, v.State)
						}
					}
				}
				if v, _ := s.Own(p.ID); math.Abs(v.State.Z-z) > 1e-9 || v.State.X != arenaX || v.State.Y != 0 || !v.State.OnGround {
					t.Fatalf("landed at %+v, want back on the ground at z %v", v.State, z)
				}
			})
		})
	}
}

// A player whose position is lost to NaN is taken back to the map's spawn, alive with nothing lost, and checkpointed there.
func TestAPlayerWhoFallsOutOfTheMapIsTakenBackToItsSpawn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		cps := &worldtest.Checkpoints{}
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), cps, worldtest.Options{})
		defer stop()
		tick(1)
		p := warriorAt(math.NaN())
		s := join(t, inst, p)
		hp := s.All()[0].(model.Snapshot).Players[0].HP
		worldtest.Walk(inst, p.ID, 0, sim.Input{}, 5)
		v, _ := s.Own(p.ID)
		if v.State.X != arenaX || v.State.Z != spawnZ || v.State.Y != 0 || !v.State.OnGround {
			t.Fatalf("at %+v, want standing at the spawn", v.State)
		}
		if v.Dead || v.HP != hp || len(worldtest.Of[model.Died](s)) != 0 || len(worldtest.Of[model.Respawned](s)) != 0 {
			t.Fatalf("view %+v after the fall, want alive with %d HP and no death", v, hp)
		}
		done := make(chan struct{})
		if err := inst.Leave(p.ID, "", done); err != nil {
			t.Fatal(err)
		}
		if err := inst.Wait(context.Background(), done); err != nil {
			t.Fatal(err)
		}
		saved := cps.Drain()
		if last := saved[len(saved)-1]; last.X != arenaX || last.Z != spawnZ {
			t.Fatalf("checkpointed at (%v, %v), want the spawn", last.X, last.Z)
		}
	})
}
