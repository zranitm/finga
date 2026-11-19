package mobs

import (
	"math/rand/v2"
	"testing"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// homing is King Slime awake and walking home from 12 m south of its spot, 3 m round (30, 30) on an open meadow, and a player.
func homing(t *testing.T) (*Arena, *mob, Body) {
	t.Helper()
	data := gamedatatest.Shipped(t)
	g := sim.Geometry{SizeX: 40, SizeZ: 40, Terrain: sim.NewTerrain(40, 2, 2, []float64{0, 0, 0, 0}, 0.7), Cylinders: []sim.Cylinder{}}
	m := &gamedata.Map{ID: "meadow", Kind: gamedata.Hunting, Geometry: g, Grid: nav.New(g, data.Movement().Step)}
	a := NewArena(Deps{Map: m, Data: data, Roll: rand.New(rand.NewPCG(1, 3))}, Tuning{TickInterval: 50 * time.Millisecond})
	def, _ := data.Mob("king_slime")
	a.SpawnBoss(def, gamedata.BossSpot{Mob: def.ID, At: gamedata.Point{X: 30, Z: 30}, Radius: 3})
	k := a.mobs[0]
	k.calm = false
	a.giveUp(k)
	k.x, k.z = 30, 18
	a.bodies.moved(k)
	return a, k, Body{ID: kernel.NewCharacterID(), Radius: 0.375, Height: 1.75}
}

// bit reports whether the arena's last tick landed a mob's attack on id.
func bit(a *Arena, id kernel.CharacterID) bool {
	for _, l := range a.Landings() {
		for _, h := range l.Hits {
			if h.Player == id {
				return true
			}
		}
	}
	return false
}

// A boss hit all the way home is still provoked when it gets there: it stays awake and fights on past the provoke, rather than turning calm while it chases.
func TestABossHitAllTheWayHomeStaysAwake(t *testing.T) {
	a, k, p := homing(t)
	var tick, last uint64
	for ; tick < 2000 && nav.Distance(k.at(), k.home) > k.homeR; tick++ {
		p.X, p.Z = k.x, k.z-3 // following it, behind
		if tick%20 == 0 {
			a.Strike(Strike{By: p.ID, Shape: gamedata.Shape{Kind: gamedata.AreaShape, Radius: 30}, Most: 1, Power: model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 1}, Aim: model.Aim{X: p.X, Z: p.Z}})
			last = tick
		}
		a.Step(tick+1, []Body{p}, nil)
	}
	if nav.Distance(k.at(), k.home) > k.homeR {
		t.Fatalf("the king at %v never got home", k.at())
	}
	after := false // bitten once the last blow's provoke is long over
	for end := tick + 400; tick < end; tick++ {
		a.Step(tick+1, []Body{p}, nil)
		after = after || tick > last+a.ticks(a.data.MobRules().Provoke)+40 && bit(a, p.ID)
	}
	if k.calm || !after {
		t.Fatalf("calm %v, bit after the provoke %v; want it awake, fighting the player beside it", k.calm, after)
	}
}

// A boss caught on its way home is put back at its spot, and calm there, never calm where it was caught.
func TestABossCaughtOnItsWayHomeIsPutBackAtItsSpot(t *testing.T) {
	a, k, p := homing(t)
	p.X, p.Z = 30, 27 // beside its spot
	tick := a.ticks(stuckFor) + 2
	a.Step(tick, []Body{p}, nil) // its walk home started on tick 0 and got nowhere since
	if k.at() != k.home || !k.calm || k.homing {
		t.Fatalf("the king at %v, calm %v, homing %v; want it at its spot %v, calm", k.at(), k.calm, k.homing, k.home)
	}
	for end := tick + 200; tick < end; tick++ {
		a.Step(tick+1, []Body{p}, nil)
		if bit(a, p.ID) {
			t.Fatal("the king put back at its spot bit a player beside it")
		}
	}
}
