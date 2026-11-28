package instance

import (
	"math"
	"testing"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// partedIn is two enemies on a flat map 64 m across, a at (ax, az) and b at (bx, bz), both on the ground and mid-walk through nothing, after a tick's parting.
func partedIn(t *testing.T, ax, az, bx, bz float64, mutate func(a, b *player)) (a, b *player) {
	t.Helper()
	data := gamedatatest.Shipped(t)
	a, b = &player{state: sim.State{X: ax, Z: az, OnGround: true}}, &player{state: sim.State{X: bx, Z: bz, OnGround: true}}
	a.ID, b.ID = kernel.NewCharacterID(), kernel.NewCharacterID()
	if mutate != nil {
		mutate(a, b)
	}
	i := &Instance{
		m: &gamedata.Map{Geometry: flat64()}, params: data.Movement(), tick: 5,
		players: map[kernel.CharacterID]*player{a.ID: a, b.ID: b}, order: []kernel.CharacterID{a.ID, b.ID},
		match: &match{phase: model.PhaseFight, fighters: map[kernel.CharacterID]*fighter{a.ID: {side: 0}, b.ID: {side: 1}}},
	}
	i.part()
	return a, b
}

// An enemy pushed against the map's edge can't be moved, so the other takes the rest of the parting, to its full distance.
func TestPartingStoppedByTheMapsEdgeLeavesTheRestToTheOther(t *testing.T) {
	data := gamedatatest.Shipped(t)
	edge := 64 - data.Movement().Radius
	a, b := partedIn(t, 30, edge, 30, edge-0.3, nil)
	if d := math.Hypot(a.state.X-b.state.X, a.state.Z-b.state.Z); math.Abs(d-0.5) > 1e-6 {
		t.Fatalf("at the edge they were parted to %.4f m; want 0.5, the free one taking all", d)
	}
	if a.state.Z != edge {
		t.Fatalf("the one at the edge moved to z %v; want it held at %v", a.state.Z, edge)
	}
}

// Two enemies in the open are parted half each, to 0.25 m into each other, and two no deeper are left.
func TestPartingPushesEnemiesHalfEachBackToTheDepth(t *testing.T) {
	a, b := partedIn(t, 30, 30, 30, 30.3, nil)
	if d := b.state.Z - a.state.Z; math.Abs(d-0.5) > 1e-6 || math.Abs(a.state.Z-29.9) > 1e-6 || math.Abs(b.state.Z-30.4) > 1e-6 {
		t.Fatalf("parted to a %.4f at z %.3f and b at %.3f; want 0.5 apart, 0.1 each", d, a.state.Z, b.state.Z)
	}
	a, b = partedIn(t, 30, 30, 30, 30.6, nil)
	if a.state.Z != 30 || b.state.Z != 30.6 {
		t.Fatalf("two 0.6 m apart, under the depth, moved to %v and %v", a.state.Z, b.state.Z)
	}
}

// A body in a move that passes through bodies isn't parted, and a teammate or a downed one never is.
func TestPartingLeavesThroughMovesTeammatesAndTheDownedAlone(t *testing.T) {
	for name, mutate := range map[string]func(a, b *player){
		"a through move": func(a, _ *player) { a.state.Move = sim.Move{Left: 5, Through: true} },
		"a downed enemy": func(_, b *player) { b.dead = true },
	} {
		a, b := partedIn(t, 30, 30, 30, 30.2, mutate)
		if a.state.Z != 30 || b.state.Z != 30.2 {
			t.Fatalf("%s: moved to %v and %v", name, a.state.Z, b.state.Z)
		}
	}
	data := gamedatatest.Shipped(t)
	a, b := &player{state: sim.State{X: 30, Z: 30}}, &player{state: sim.State{X: 30, Z: 30.2}}
	a.ID, b.ID = kernel.NewCharacterID(), kernel.NewCharacterID()
	i := &Instance{m: &gamedata.Map{Geometry: flat64()}, params: data.Movement(), players: map[kernel.CharacterID]*player{a.ID: a, b.ID: b}, order: []kernel.CharacterID{a.ID, b.ID},
		match: &match{phase: model.PhaseFight, fighters: map[kernel.CharacterID]*fighter{a.ID: {side: 0}, b.ID: {side: 0}}}}
	i.part()
	if a.state.Z != 30 || b.state.Z != 30.2 {
		t.Fatalf("teammates moved to %v and %v", a.state.Z, b.state.Z)
	}
}

// flat64 is flat ground 64 m across.
func flat64() sim.Geometry {
	return sim.Indexed(sim.Geometry{SizeX: 64, SizeZ: 64, Terrain: sim.NewTerrain(64, 2, 2, []float64{0, 0, 0, 0}, 0.7), Boxes: []sim.Box{}, Cylinders: []sim.Cylinder{}})
}

// A shove meets the shoved player's enemies where they stand and no teammate: it stops with their centres 0.75 m apart.
func TestAShoveStopsAtTheShovedsEnemiesAndNoTeammate(t *testing.T) {
	data := gamedatatest.Shipped(t)
	mk := func(x, z float64) *player {
		p := &player{state: sim.State{X: x, Z: z, OnGround: true}}
		p.ID = kernel.NewCharacterID()
		return p
	}
	shoved, enemy, mate := mk(30, 30), mk(30, 31.5), mk(30, 31.2)
	i := &Instance{
		m: &gamedata.Map{Geometry: flat64()}, params: data.Movement(),
		players: map[kernel.CharacterID]*player{shoved.ID: shoved, enemy.ID: enemy, mate.ID: mate}, order: []kernel.CharacterID{shoved.ID, enemy.ID, mate.ID},
		match: &match{phase: model.PhaseFight, fighters: map[kernel.CharacterID]*fighter{shoved.ID: {side: 0}, mate.ID: {side: 0}, enemy.ID: {side: 1}}},
	}
	g := i.geometry(shoved).Blocking(i.foesNow(shoved))
	got := sim.Push(shoved.state, 0, 2, g, i.params)
	if d := enemy.state.Z - got.Z; math.Abs(d-0.75) > 1e-6 {
		t.Fatalf("shoved to %.3f m short of its enemy; want it stopped at 0.75, the teammate between ignored", d)
	}
	if i.foesNow(mate) == nil {
		t.Fatal("a teammate's enemies are missing")
	}
	enemy.dead = true
	if got := sim.Push(shoved.state, 0, 2, i.geometry(shoved).Blocking(i.foesNow(shoved)), i.params); got.Z < 31.9 {
		t.Fatalf("shoved only to z %.3f past a downed enemy; want the whole 2 m", got.Z)
	}
}

// Players on a match's map are never held to every second tick, whatever the crowd: an enemy's missed tick would leave a blocker guessed, as a mob's would.
func TestAMatchsPlayersAreNeverSteppedEverySecondTick(t *testing.T) {
	for _, inMatch := range []bool{false, true} {
		in := gamedata.Interest{Cell: 32, Behind: 1, Ahead: 3, Side: 3, Crowd: 2}
		i := &Instance{interest: newInterest(in, 320, 320), players: map[kernel.CharacterID]*player{}, mobSent: map[uint64]sentMob{}, tick: 7}
		if inMatch {
			i.match = &match{}
		}
		var views []model.PlayerView
		for k := range 3 {
			p := &player{}
			p.ID = kernel.NewCharacterID()
			v := model.PlayerView{ID: p.ID, Slot: uint32(k), State: sim.State{X: 40 + float64(k), Z: 40, OnGround: true}}
			p.sent = sentPlayerOf(v)
			i.players[p.ID] = p
			i.interest.place(playerOf(p.ID), v.State.X, v.State.Z)
			v.State.X += 0.1 // each walks on
			views = append(views, v)
		}
		i.interest.crossed = nil
		_, steps, _ := i.stepPlayers(views)
		if held := len(views) - len(steps); inMatch && held != 0 || !inMatch && held == 0 {
			t.Fatalf("in a match %v: %d of 3 players' steps held a tick on an odd tick in a crowd of 3 over 2; want none in a match, some elsewhere", inMatch, held)
		}
	}
}
