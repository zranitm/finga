package instance

import (
	"testing"
	"time"

	"finga/internal/application/world/mobs"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/sim"
)

// A map nobody is on keeps no mob baselines: a mob killed by a poison left ticking is in no frame's removed list, and nobody holds the views the rest were stepped from.
func TestAnEmptyMapKeepsNoMobBaselines(t *testing.T) {
	m := &gamedata.Map{ID: "empty", Geometry: sim.Geometry{SizeX: 64, SizeZ: 64, Terrain: sim.NewTerrain(64, 2, 2, []float64{0, 0, 0, 0}, 0.7), Boxes: []sim.Box{}, Cylinders: []sim.Cylinder{}}}
	data := gamedatatest.Shipped(t)
	i := &Instance{
		m:        m,
		arena:    mobs.NewArena(mobs.Deps{Map: m, Data: data}, mobs.Tuning{TickInterval: 50 * time.Millisecond}),
		interest: newInterest(gamedata.Interest{Cell: 32, Behind: 1, Ahead: 1, Side: 1}, 64, 64),
		mobSent:  map[uint64]sentMob{7: {}, 8: {}},
	}
	i.broadcast(nil)
	if len(i.mobSent) != 0 {
		t.Fatalf("%d baselines kept on a map with no one on it", len(i.mobSent))
	}
}
