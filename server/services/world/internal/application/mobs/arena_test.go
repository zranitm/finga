package mobs_test

import (
	"testing"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/application/mobs"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

const tick = 50 * time.Millisecond

// A slime area on a long platform: slimes are 32 px wide, so they patrol from x 116 to 284 and spawn, at a roll of 0.5, at 200.
const (
	minX, spawnX, maxX = 116.0, 200.0, 284.0
	floor              = 400.0
)

func slimes(respawn time.Duration) *gamedata.Map {
	return &gamedata.Map{
		ID: "meadow", Kind: gamedata.Hunting,
		Geometry: sim.Geometry{Platforms: []model.Rect{{X: 0, Y: floor, W: 1000, H: 16}}},
		Spawns:   []gamedata.Spawn{{Mob: "green_slime", Max: 2, Respawn: respawn, Area: model.Rect{X: 100, Y: 300, W: 200, H: 100}}},
	}
}

func newArena(t *testing.T, roll float64) *mobs.Arena {
	t.Helper()
	return mobs.NewArena(mobs.Deps{Map: slimes(5 * time.Second), Data: worldtest.LoadData(t), Roll: worldtest.FixedRoll{V: roll}}, mobs.Tuning{TickInterval: tick})
}

// strong hits whatever it reaches, for more than a slime's HP.
var strong = model.Attacker{AttackPower: 1000, Accuracy: 100}

func meleeAt(x float64, facing int) mobs.Strike {
	return mobs.Strike{By: kernel.NewCharacterID(), Attack: gamedata.Attack{Kind: gamedata.Melee, Width: 60, Height: 60}, Power: strong, X: x, Y: floor, Facing: facing, Height: 60}
}

func TestTheArenaIsFullBeforeItsFirstTick(t *testing.T) {
	views := newArena(t, 0.5).Views()
	if len(views) != 2 || views[0].ID == views[1].ID {
		t.Fatalf("views %+v; want the area's two slimes", views)
	}
	for _, v := range views {
		if v.Type != "green_slime" || v.X != spawnX || v.Y != floor || v.HP != 30 || v.MaxHP != 30 || v.Facing != 1 {
			t.Fatalf("spawned %+v", v)
		}
	}
}

// A slime walks to its platform's edge within the area, waits there a second, then turns and walks to the other edge.
func TestAMobPatrolsAndPausesAtEachEdge(t *testing.T) {
	a := newArena(t, 0.5)
	var xs []float64
	var facings []int
	for n := range uint64(200) {
		a.Step(n + 1)
		v := a.Views()[0]
		xs, facings = append(xs, v.X), append(facings, v.Facing)
	}
	pause := int(time.Second / tick)
	at := 0
	for _, edge := range []struct {
		x      float64
		facing int
	}{{maxX, -1}, {minX, 1}} {
		for at < len(xs) && xs[at] != edge.x {
			at++
		}
		if at+pause >= len(xs) {
			t.Fatalf("never reached %v: %v", edge.x, xs)
		}
		for k := at; k < at+pause; k++ {
			if xs[k] != edge.x || facings[k] != edge.facing {
				t.Fatalf("tick %d at %v facing %d; want a second's pause at %v facing %d", k+1, xs[k], facings[k], edge.x, edge.facing)
			}
		}
		if moved := xs[at+pause] - edge.x; moved*float64(edge.facing) <= 0 {
			t.Fatalf("after the pause it went from %v to %v; want back the way it now faces", edge.x, xs[at+pause])
		}
		at += pause
	}
}

func TestAKilledMobComesBackAfterItsAreasRespawn(t *testing.T) {
	a := newArena(t, 0.5)
	a.Step(1)
	_, _ = a.Changes()
	stood := a.Views()[0]
	blow, reached := a.Strike(meleeAt(stood.X-20, 1))
	if !reached || blow.Killed == nil || blow.Killed.Mob.ID != "green_slime" || blow.Killed.X != stood.X || blow.Killed.Y != floor {
		t.Fatalf("blow %+v reached %v; want the slime killed where it stood", blow, reached)
	}
	if _, removed := a.Changes(); len(removed) != 1 || removed[0] != blow.Damage.Target.Mob {
		t.Fatalf("removed %v; want the killed slime", removed)
	}
	back := 1 + uint64(5*time.Second/tick)
	for n := uint64(2); n < back; n++ {
		a.Step(n)
		if got := len(a.Views()); got != 1 {
			t.Fatalf("tick %d: %d slimes; want one until the respawn", n, got)
		}
	}
	a.Step(back)
	views := a.Views()
	if len(views) != 2 || views[1].ID == blow.Damage.Target.Mob {
		t.Fatalf("tick %d: %+v; want a new slime back", back, views)
	}
}

func TestAStrikeReachesOnlyWhatItsAttackCovers(t *testing.T) {
	ranged := func(x float64, rng float64) mobs.Strike {
		return mobs.Strike{By: kernel.NewCharacterID(), Attack: gamedata.Attack{Kind: gamedata.Ranged, Range: rng}, Power: strong, X: x, Y: floor, Facing: 1, Height: 40}
	}
	tests := []struct {
		name    string
		strike  mobs.Strike
		reached bool
	}{
		{"a swing in front of the slime", meleeAt(spawnX-20, 1), true},
		{"a swing facing away", meleeAt(spawnX-20, -1), false},
		{"a swing out of reach", meleeAt(spawnX-100, 1), false},
		{"a shot in range", ranged(0, 300), true},
		{"a shot that falls short", ranged(0, 150), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blow, reached := newArena(t, 0.5).Strike(tt.strike)
			if reached != tt.reached {
				t.Fatalf("reached %v, want %v", reached, tt.reached)
			}
			if reached && (blow.Damage.Source.Player != tt.strike.By || !blow.Damage.Target.IsMob() || blow.Damage.Amount < 30) {
				t.Fatalf("damage %+v", blow.Damage)
			}
		})
	}
}

// A strike that misses still reaches its slime, which keeps its HP.
func TestAMissHurtsNothing(t *testing.T) {
	a := newArena(t, 0.995) // past the best hit chance
	blow, reached := a.Strike(meleeAt(a.Views()[0].X-20, 1))
	if !reached || !blow.Damage.Miss || blow.Damage.Amount != 0 || blow.Killed != nil {
		t.Fatalf("blow %+v reached %v; want a miss", blow, reached)
	}
	for _, v := range a.Views() {
		if v.HP != v.MaxHP {
			t.Fatalf("a slime lost HP to a miss: %+v", v)
		}
	}
}

func TestTouchHurtsAPlayerOverlappingAMob(t *testing.T) {
	a := newArena(t, 0.5)
	id := kernel.NewCharacterID()
	dealt, mobX, touched := a.Touch(id, model.Rect{X: spawnX, Y: floor - 50, W: 30, H: 50}, model.Defender{})
	if !touched || mobX != spawnX || dealt.Target.Player != id || !dealt.Source.IsMob() || dealt.Amount == 0 {
		t.Fatalf("dealt %+v from %v touched %v; want the slime's hit", dealt, mobX, touched)
	}
	if _, _, touched := a.Touch(id, model.Rect{X: 600, Y: floor - 50, W: 30, H: 50}, model.Defender{}); touched {
		t.Fatal("a player far from every slime was touched")
	}
}

func TestChangesTellEachChangeOnce(t *testing.T) {
	a := newArena(t, 0.5)
	if changed, _ := a.Changes(); len(changed) != 2 {
		t.Fatalf("changed %+v; want both spawns", changed)
	}
	if changed, removed := a.Changes(); len(changed) != 0 || len(removed) != 0 {
		t.Fatalf("changed %+v, removed %v; want nothing told twice", changed, removed)
	}
	a.Step(1)
	if changed, _ := a.Changes(); len(changed) != 2 || changed[0].X == spawnX {
		t.Fatalf("changed %+v; want both slimes moved", changed)
	}
}
