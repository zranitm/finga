package instance_test

import (
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
)

// large-regions.md section 7: walking out of combat 2 s, 40 inputs of 3 sub-steps, breaks into a sprint from the 41st, stepped as the sim steps it, its count on the player's view.
// An attack pressed with two inputs still queued ends the count from the first input past them, as the client ends it from the first it sends after the press.
func TestWalkingBreaksIntoASprintAndAPressEndsIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data, m := worldtest.LoadData(t), arena(t, time.Hour)
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := warriorAt(spawnZ) // 17 m from the slime, out of its sight
		s := join(t, inst, p)
		warrior, _ := data.Class(kernel.Class("warrior"))
		derived := instance.StatsOf(data, warrior, 1, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived
		params := data.Movement().Faster(sim.SpeedFactor(derived.MoveSpeed))
		east := sim.Input{Right: true}
		walked := func(from sim.State, inputs int) sim.State {
			for range inputs {
				from = stepped(from, east, m.Geometry, params)
			}
			return from
		}
		v, _ := s.Own(p.ID)
		start := v.State
		seq := worldtest.Walk(inst, p.ID, 0, east, 40)
		if got, want := viewAfter(t, s, p.ID, seq), walked(start, 40); got != want || got.Sprint != 120 || got.VX != params.WalkSpeed {
			t.Fatalf("after 2 s walking east %+v; want %+v, its count at 120, still at the walk", got, want)
		}
		seq = worldtest.Walk(inst, p.ID, seq, east, 1)
		sprinting := viewAfter(t, s, p.ID, seq)
		if want := walked(start, 41); sprinting != want || sprinting.VX != float64(params.WalkSpeed*1.6) {
			t.Fatalf("an input on %+v; want %+v, sprinting at 1.6 times the walk", sprinting, want)
		}

		for k := uint32(1); k <= 2; k++ {
			_ = inst.Input(p.ID, seq+k, east, 0)
		}
		_ = inst.Attack(p.ID, 0, 0) // north, at nothing in reach
		seq = worldtest.Walk(inst, p.ID, seq+2, east, 2)
		tick(2) // the two queued before the press step too
		want := sim.EndSprint(walked(sprinting, 2))
		want = walked(want, 2)
		if got := viewAfter(t, s, p.ID, seq); got != want || got.Sprint != 6 || got.VX != params.WalkSpeed {
			t.Fatalf("an attack pressed with two inputs queued, then two more: %+v; want %+v, at the walk from the third, counting again", got, want)
		}
	})
}

// A fight event keeps a player in combat 100 ticks from the next, which the world steps with no sprint; the count starts on the first tick out of it, as the client's own clock of the same Damage starts it.
// Ayla throws from 6 m and kills the one-HP slime, the map's only mob, then walks east.
func TestAPlayerInCombatWalksWithNoSprint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := skillDataWith(t, map[string]func(string) string{"mobs.yaml": edit(t, "hp: 34", "hp: 1")})
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		ayla := caster(farFromSlime)
		ayla.Skills.Points["throw"] = 1
		s := join(t, inst, ayla)
		if err := cast(inst, ayla.ID, "throw"); err != nil {
			t.Fatal(err)
		}
		tick(20) // the throw lands
		hits := damages(s)
		if len(hits) != 1 || hits[0].Source.Player != ayla.ID {
			t.Fatalf("damage %+v; want Ayla's one throw", hits)
		}
		out := hits[0].Tick + uint64(data.Combat()/worldtest.TickInterval)
		worldtest.Walk(inst, ayla.ID, 0, sim.Input{Right: true}, int(out-hits[0].Tick)+2)
		var counted []uint64
		for _, d := range s.Deltas() {
			for _, v := range d.Players {
				if v.ID == ayla.ID && v.State.Sprint > 0 {
					counted = append(counted, d.Tick)
				}
			}
		}
		if len(counted) == 0 || counted[0] != out {
			t.Fatalf("the count first ran at ticks %v; want from %d, the first out of combat", counted, out)
		}
	})
}
