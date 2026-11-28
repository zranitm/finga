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

// A cell goes slow past the crowd and fast again only at 4/5 of it, so a crowd's edge doesn't flip each tick; the cells round it count.
func TestACrowdedCellKeepsItsTierBetweenTheCrowdAndFourFifthsOfIt(t *testing.T) {
	n := newInterest(gamedata.Interest{Cell: 32, Behind: 1, Ahead: 1, Side: 1, Crowd: 10}, 320, 320)
	mid, beside := n.of(100, 100), n.of(132, 100)
	tick := uint64(0)
	for _, c := range []struct {
		there, next int // players in the cell, and in the one beside it
		slow        bool
	}{{11, 0, true}, {9, 0, true}, {5, 4, true}, {8, 0, false}, {9, 0, false}, {5, 6, true}, {0, 0, false}} {
		n.players[mid], n.players[beside] = c.there, c.next
		tick++
		if got := n.crowded(mid, tick); got != c.slow {
			t.Fatalf("%d players in the cell and %d beside it: slow %v, want %v", c.there, c.next, got, c.slow)
		}
	}
}
