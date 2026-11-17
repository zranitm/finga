package mobs

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// A pile chasing a player who keeps moving attacks once it can: a mob with an attack ready that reaches them starts it within 12 s. Two walking deep in each other drift apart, so the crowding rule can't hold one off for good; before, a gloom slime by a Shade Lord waited 27.6 s, and a sun slime by vultures 23.2 s.
func TestAPileChasingAMovingPlayerAttacksOnceItCan(t *testing.T) {
	const most = 12 * time.Second
	data := gamedatatest.Shipped(t)
	g := sim.Geometry{SizeX: 40, SizeZ: 40, Terrain: sim.NewTerrain(40, 2, 2, []float64{0, 0, 0, 0}, 0.7), Cylinders: []sim.Cylinder{}}
	grid := nav.New(g, data.Movement().Step)
	pile := func(mob string, n int, r float64) gamedata.Spawn {
		return gamedata.Spawn{Mob: mob, Max: n, Respawn: 5 * time.Second, At: gamedata.Point{X: 20, Z: 20}, Radius: r}
	}
	for _, tt := range []struct {
		name   string
		spawns []gamedata.Spawn
		boss   string
	}{
		{"a Shade Lord with shade bats and gloom slimes", []gamedata.Spawn{pile("shade_bat", 2, 1), pile("gloom_slime", 2, 1)}, "shade_lord"},
		{"dust vultures and sun slimes", []gamedata.Spawn{pile("dust_vulture", 4, 1), pile("sun_slime", 2, 1)}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for seed := uint64(1); seed <= 20; seed++ {
				m := &gamedata.Map{ID: "meadow", Kind: gamedata.Hunting, Geometry: g, Spawns: tt.spawns, Grid: func() *nav.Grid { return grid }}
				a := NewArena(Deps{Map: m, Data: data, Roll: rand.New(rand.NewPCG(seed, 3))}, Tuning{TickInterval: 50 * time.Millisecond})
				if tt.boss != "" {
					def, _ := data.Mob(tt.boss)
					a.SpawnBoss(def, gamedata.BossSpot{Mob: tt.boss, At: gamedata.Point{X: 20.5, Z: 20.5}, Radius: 1})
				}
				p := Body{ID: kernel.NewCharacterID()}
				p.Radius, p.Height = 0.375, 1.75
				since := map[uint64]uint64{} // by mob, the tick it last had an attack ready that reaches them, with none started since
				for n := uint64(1); n <= 600; n++ {
					th := float64(n) * 0.02 // round the pile 5 m out, 2 m/s
					p.X, p.Z = 20+5*math.Sin(th), 20+5*math.Cos(th)
					a.Step(n, []Body{p})
					for _, mb := range a.mobs {
						ready := false
						for k := range mb.def.Attacks {
							ready = ready || mb.ready[k] <= n && a.reaches(mb, k, p)
						}
						if mb.attack >= 0 || !ready {
							delete(since, mb.id)
							continue
						}
						if _, ok := since[mb.id]; !ok {
							since[mb.id] = n
						}
						if waited := a.ticks(most); n-since[mb.id] > waited {
							t.Fatalf("seed %d tick %d: the %s at (%.2f, %.2f) has had an attack ready that reaches the player for %v without starting it; want it started within %v", seed, n, mb.def.ID, mb.x, mb.z, time.Duration(n-since[mb.id])*50*time.Millisecond, most)
						}
					}
				}
			}
		})
	}
}
