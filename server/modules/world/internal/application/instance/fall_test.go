package instance_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/sim"
	"finga/modules/world/internal/model"
	"finga/modules/world/worldtest"
)

// cliffs is sandstone_cliffs' east end: block_2's face rises at x 2016 from block_1, where a still slime stands just west of x 2004.
func cliffs() *gamedata.Map {
	return &gamedata.Map{
		ID: "cliffs", Kind: gamedata.Hunting, Town: "town", Width: 2560, Height: 1024,
		Geometry: sim.Geometry{Solids: []sim.Rect{
			{X: 0, Y: 992, W: 2560, H: 32},    // ground
			{X: 0, Y: 0, W: 32, H: 992},       // left_wall
			{X: 2528, Y: 0, W: 32, H: 992},    // right_wall
			{X: 1536, Y: 896, W: 992, H: 96},  // block_1
			{X: 2016, Y: 800, W: 512, H: 192}, // block_2
		}},
		Spawns:      []gamedata.Spawn{{Mob: "green_slime", Max: 1, Respawn: time.Hour, Area: sim.Rect{X: 1964, Y: 868, W: 32, H: 28}}},
		PlayerSpawn: gamedata.Point{X: 112, Y: 992},
	}
}

// walled is the old maps' west end: a wall up to x 32, with a still slime just east of x 44.
func walled() *gamedata.Map {
	return &gamedata.Map{
		ID: "walled", Kind: gamedata.Hunting, Town: "town", Width: 1280, Height: 640,
		Geometry: sim.Geometry{Solids: []sim.Rect{
			{X: 0, Y: 608, W: 1280, H: 32},
			{X: 0, Y: 0, W: 32, H: 608},
		}},
		Spawns:      []gamedata.Spawn{{Mob: "green_slime", Max: 1, Respawn: time.Hour, Area: sim.Rect{X: 48, Y: 580, W: 32, H: 28}}},
		PlayerSpawn: gamedata.Point{X: 112, Y: 608},
	}
}

// Knockback into a wall stops at its face: it used to end 16 px inside, and the hop's rise then put the player under the wall and out of the world.
func TestKnockbackStopsAtAWall(t *testing.T) {
	for _, c := range []struct {
		name string
		m    *gamedata.Map
		x, y float64 // touching both the slime and the wall
	}{
		{"east into a cliff face", cliffs(), 2004, 896},
		{"west into the map's wall", walled(), 44, 608},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				inst, stop := worldtest.RunMap(t, data, c.m, nil, worldtest.Options{})
				defer stop()
				time.Sleep(worldtest.TickInterval)
				synctest.Wait()
				p := warriorAt(c.x)
				p.Y = c.y
				s := join(t, inst, p)
				mobHitsOn(t, s, p.ID, 1)
				tick(15) // the knockback's hop and the landing, inside the hit guard
				if got := damages(s); len(got) != 1 || got[0].Target.Player != p.ID {
					t.Fatalf("damages %+v, want the slime's one hit", got)
				}
				for _, d := range s.Deltas() {
					for _, v := range d.Players {
						if !c.m.Holds(v.State.X, v.State.Y, data.Movement()) {
							t.Fatalf("tick %d: the player stands at (%v, %v), inside a solid or off the map", d.Tick, v.State.X, v.State.Y)
						}
					}
				}
				if v, _ := s.Own(p.ID); v.State.X != c.x || v.State.Y != c.y || !v.State.OnGround {
					t.Fatalf("landed at %+v, want back on the ground at (%v, %v)", v.State, c.x, c.y)
				}
			})
		})
	}
}

// A player who falls out of the map is taken back to its spawn, alive with nothing lost, and checkpointed there.
func TestAPlayerWhoFallsOutOfTheMapIsTakenBackToItsSpawn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		ledge := &gamedata.Map{
			ID: "ledge", Kind: gamedata.Hunting, Town: "town", Width: 1280, Height: 640,
			Geometry:    sim.Geometry{Solids: []sim.Rect{{X: 0, Y: 608, W: 640, H: 32}}},
			PlayerSpawn: gamedata.Point{X: 100, Y: 608},
		}
		cps := &worldtest.Checkpoints{}
		inst, stop := worldtest.RunMap(t, data, ledge, cps, worldtest.Options{})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(1000) // past the ledge's end, over nothing
		s := join(t, inst, p)
		hp := s.All()[0].(model.Snapshot).Players[0].HP
		worldtest.Walk(inst, p.ID, 0, sim.Input{}, 20)
		v, _ := s.Own(p.ID)
		if v.State.X != 100 || v.State.Y != 608 || !v.State.OnGround {
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
		if last := saved[len(saved)-1]; last.X != 100 || last.Y != 608 {
			t.Fatalf("checkpointed at (%v, %v), want the spawn", last.X, last.Y)
		}
	})
}
