package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

func TestKeysWalkTheWayNearestTheWayWanted(t *testing.T) {
	for _, c := range []struct {
		dx, dz float64
		want   *clientv1.Input
	}{
		{1, 0, &clientv1.Input{Right: true}},
		{0, -2, &clientv1.Input{Down: true}},
		{1, 1, &clientv1.Input{Right: true, Up: true}},
		{-1, -1, &clientv1.Input{Left: true, Down: true}},
		{1, 0.3, &clientv1.Input{Right: true}},           // 17° off east
		{1, 0.5, &clientv1.Input{Right: true, Up: true}}, // 27° off east, nearer north-east
		{0, 0, &clientv1.Input{}},
	} {
		got := keysToward(c.dx, c.dz)
		if got.GetLeft() != c.want.GetLeft() || got.GetRight() != c.want.GetRight() || got.GetUp() != c.want.GetUp() || got.GetDown() != c.want.GetDown() {
			t.Errorf("toward (%v, %v) presses %v, want %v", c.dx, c.dz, got, c.want)
		}
	}
	if a := yawTo(nav.Point{}, nav.Point{X: 0, Z: 1}); a != 0 {
		t.Errorf("north aims %v", a)
	}
	if a := yawTo(nav.Point{}, nav.Point{X: 1, Z: 0}); math.Abs(float64(a)-math.Pi/2) > 1e-6 {
		t.Errorf("east aims %v, want π/2", a)
	}
}

func TestAStallIsAWayPressedOnTheGroundThatGoesNowhere(t *testing.T) {
	right, still := &clientv1.Input{Right: true}, &clientv1.Input{}
	at := nav.Point{X: 5, Z: 5}
	for _, c := range []struct {
		name     string
		onGround bool
		held     *clientv1.Input
		want     bool
	}{
		{"against a wall", true, right, true},
		{"in the air, its updates late", false, right, false},
		{"standing to attack", true, still, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := stall{at: at}
			s.stalled(at, c.onGround, c.held) // the way pressed, which counts from the next decision
			for k := range 2 * stallFor {
				if got := s.stalled(at, c.onGround, c.held); got != (c.want && k%stallFor == stallFor-1) {
					t.Fatalf("decision %d: stalled %v; a stall is told once, then counted again", k+1, got)
				}
			}
		})
	}
	s := stall{at: at}
	for range stallFor {
		s.stalled(at, true, right)
	}
	for k := range stallFor - 1 {
		if s.stalled(nav.Point{X: 5.1, Z: 5}, true, right) {
			t.Fatalf("decision %d after a step on: stalled; the step starts the count again", k+1)
		}
	}
	s = stall{at: at}
	for range stallFor {
		s.stalled(at, true, right)
	}
	if s.stalled(at, true, &clientv1.Input{Up: true}) {
		t.Fatal("stalled the decision a new way was pressed; its first steps take a round trip to show")
	}
}

func TestAStalledWalkerJumpsThenStepsAsideEachSideInTurn(t *testing.T) {
	data, w := testWorld(t)
	m, r := w.maps["town"], data.Movement().Radius
	from, to, now := point(m.PlayerSpawn), point(m.PlayerSpawn), time.Unix(1_000_000, 0)
	to.X += 4
	var wk walker
	ahead := wayOf(wk.keys(m, r, from, to, now, false))
	jump := wk.keys(m, r, from, to, now, true)
	if !jump.GetJump() || wayOf(jump) != ahead {
		t.Fatalf("the first stall presses %v, want a jump ahead %v", jump, ahead)
	}
	aside := wayOf(wk.keys(m, r, from, to, now, true))
	if aside == ([2]int{}) || aside[0]*ahead[0]+aside[1]*ahead[1] != 0 {
		t.Fatalf("the second stall steps %v, want across the way ahead %v", aside, ahead)
	}
	for k := 1; k < asideFor; k++ {
		if got := wayOf(wk.keys(m, r, from, to, now, false)); got != aside {
			t.Fatalf("decision %d of the step aside presses %v, want %v", k+1, got, aside)
		}
	}
	if got := wayOf(wk.keys(m, r, from, to, now, false)); got != ahead {
		t.Fatalf("after the step aside it presses %v, want %v", got, ahead)
	}
	if !wk.keys(m, r, from, to, now, true).GetJump() {
		t.Fatal("the third stall doesn't jump")
	}
	if got := wayOf(wk.keys(m, r, from, to, now, true)); got != [2]int{-aside[0], -aside[1]} {
		t.Fatalf("the fourth stall steps %v, want the other side of %v", got, aside)
	}
}

func TestAWalkerWithNoWayWalksStraightAndLooksAgainASecondOn(t *testing.T) {
	data, w := testWorld(t)
	m, r := w.maps["forest"], data.Movement().Radius
	from, onTop, now := nav.Point{X: 40, Z: 28}, nav.Point{X: 50, Z: 28}, time.Unix(1_000_000, 0) // a ledge only a jump gets onto
	var wk walker
	if got := wayOf(wk.keys(m, r, from, onTop, now, false)); !wk.lost() || got != [2]int{1, 0} {
		t.Fatalf("lost %v, pressing %v; want lost, straight east", wk.lost(), got)
	}
	if wk.keys(m, r, from, onTop, now.Add(repathEvery/2), true); wk.found != now {
		t.Fatal("a stall with no way looked again within the second")
	}
	if wk.keys(m, r, from, nav.Point{X: 30, Z: 20}, now.Add(repathEvery/2), false); wk.lost() {
		t.Fatal("still lost once its end moved to open ground")
	}
}

// The bots walk their maps by the game's own movement: from where players appear, and from each portal, to every portal, NPC and point a hunt roams to that it has a way to.
func TestABotWalksToEveryPortalNPCAndHuntOnItsMaps(t *testing.T) {
	data, w := testWorld(t)
	mv := data.Movement()
	b := &bot{data: data, rng: rand.New(rand.NewPCG(1, 2))}
	for id, m := range w.maps {
		type target struct {
			name   string
			at     nav.Point
			within float64
			roam   bool
		}
		var targets []target
		for _, p := range m.Portals {
			targets = append(targets, target{name: "portal " + p.Name, at: point(p.At), within: p.Radius / 2})
		}
		for _, n := range townNPCs(data, m) {
			targets = append(targets, target{name: "NPC " + n.ID, at: point(n.At), within: b.talkWithin()})
		}
		b.mapID = id
		for i, sp := range m.Spawns {
			s := spot{mapID: id, at: point(sp.At), radius: sp.Radius}
			targets = append(targets, target{fmt.Sprintf("a roam in spawn %d's %s", i, sp.Mob), b.roll(s), 1, true})
		}
		starts := []target{{name: "where players appear", at: point(m.PlayerSpawn)}}
		for _, p := range m.Portals {
			starts = append(starts, target{name: "portal " + p.Name, at: point(p.At)})
		}
		for _, from := range starts {
			for _, to := range targets {
				t.Run(fmt.Sprintf("%s from %s to %s", id, from.name, to.name), func(t *testing.T) {
					var finder nav.Finder
					leash := math.Hypot(m.Geometry.SizeX, m.Geometry.SizeZ)
					if len(finder.Path(m.Grid(), from.at, to.at, mv.Radius, from.at, leash)) == 0 {
						if to.roam {
							t.Skipf("no way to %+v: the hunt rolls again", to.at)
						}
						t.Fatalf("no way to %+v", to.at)
					}
					if took, ok := walkOffline(m, mv, from.at, to.at, to.within); !ok {
						t.Fatalf("not within %.2f m of %+v after %s", to.within, to.at, took)
					}
				})
			}
		}
	}
}

// walkOffline steps the game's movement from a point on m toward another, deciding as a bot does 20 times a second, till within metres of it; false when a minute and twice the straight walk runs out first.
func walkOffline(m *gamedata.Map, mv sim.Params, from, to nav.Point, within float64) (time.Duration, bool) {
	const steps = 3 // of mv.Dt to a decision
	s := sim.State{X: from.X, Y: sim.Ground(from.X, from.Z, math.Inf(1), m.Geometry), Z: from.Z, OnGround: true}
	var walk walker
	stuck, held := stall{at: from}, &clientv1.Input{}
	limit := time.Minute + time.Duration(2*nav.Distance(from, to)/mv.WalkSpeed*float64(time.Second))
	now := time.Unix(1_000_000, 0)
	for took := time.Duration(0); took < limit; took += time.Duration(steps * mv.Dt * float64(time.Second)) {
		at := nav.Point{X: s.X, Z: s.Z}
		if nav.Distance(at, to) <= within {
			return took, true
		}
		held = walk.keys(m, mv.Radius, at, to, now.Add(took), stuck.stalled(at, s.OnGround, held))
		for range steps {
			s = sim.Step(s, sim.Input{Left: held.GetLeft(), Right: held.GetRight(), Up: held.GetUp(), Down: held.GetDown(), Jump: held.GetJump()}, m.Geometry, mv)
		}
	}
	return limit, false
}

// C12: with a field boss up, a bot hunts its map clear of where the boss could see it, and waits in the town while the boss is near its way or its goal.
func TestABotKeepsClearOfAFieldBoss(t *testing.T) {
	data, w := testWorld(t)
	b := &bot{data: data, world: w, mapID: "forest", rng: rand.New(rand.NewPCG(1, 2))}
	at, r := point(w.maps["forest"].Boss.At), w.maps["forest"].Boss.Radius
	if b.nearBoss("forest", at) {
		t.Fatal("near a boss that isn't up")
	}
	b.bossUntil("forest", time.Now().Add(time.Minute))
	clear := data.MobRules().Sight + bossMargin
	for _, c := range []struct {
		name string
		seen bool
		at   nav.Point
		want bool
	}{
		{"in its sight", true, nav.Point{X: at.X - clear + 0.5, Z: at.Z}, true},
		{"past it", true, nav.Point{X: at.X - clear - 0.5, Z: at.Z}, false},
		{"on a diagonal, as the bot keeps a square clear", true, nav.Point{X: at.X - clear + 0.5, Z: at.Z + clear - 0.5}, true},
		{"unseen, in its circle's sight", false, nav.Point{X: at.X - r - clear + 0.5, Z: at.Z}, true},
		{"unseen, past its circle's sight", false, nav.Point{X: at.X - r - clear - 0.5, Z: at.Z}, false},
	} {
		b.mobs, b.boss = nil, 0
		if c.seen {
			b.mobs, b.boss = map[uint32]*clientv1.MobView{9: {Id: 9, Type: "king_slime", X: at.X, Z: at.Z, Hp: 100}}, 9
		}
		if got := b.nearBoss("forest", c.at); got != c.want {
			t.Errorf("%s: near %v, want %v", c.name, got, c.want)
		}
	}
	slimes, _ := w.levelSpot(1)
	for range 200 {
		if p := b.roll(slimes); b.nearBoss("forest", p) {
			t.Fatalf("rolled %+v, near the boss", p)
		}
	}

	b.mapID, b.mobs, b.boss = "town", nil, 0
	cave, _ := w.levelSpot(11)
	if by, ok := b.bossBlocks(goal{kind: huntGoal, spot: slimes}); ok {
		t.Errorf("the forest's slimes, far from its boss, are blocked by %s's", by)
	}
	toCave, _ := w.portalTo("forest", "cave")
	b.mapID, b.mobs, b.boss = "forest", map[uint32]*clientv1.MobView{9: {Id: 9, Type: "king_slime", X: toCave.At.X, Z: toCave.At.Z, Hp: 100}}, 9
	if by, ok := b.bossBlocks(goal{kind: huntGoal, spot: cave}); !ok || by != "forest" {
		t.Errorf("the cave, through the forest's portal where its boss is seen, is blocked by %q's, want the forest's", by)
	}
	b.mobs, b.boss = nil, 0
	mid, _ := w.levelSpot(6)
	if !b.nearBoss("forest", mid.at) {
		t.Fatalf("the level 6 spot's centre %+v is clear of the boss; the case below wants it near", mid.at)
	}
	if by, ok := b.bossBlocks(goal{kind: huntGoal, spot: mid}); ok {
		t.Errorf("the level 6 spot, its centre near the boss but most of it clear, is blocked by %s's", by)
	}
	b.mobs, b.boss = map[uint32]*clientv1.MobView{9: {Id: 9, Type: "king_slime", X: at.X - r, Z: at.Z, Hp: 100}}, 9
	far := nav.Point{X: at.X - r + clear + 0.5, Z: at.Z} // past the boss's sight where it's seen, in its circle's
	if b.nearBoss("forest", far) {
		t.Fatalf("%+v is near the boss seen at its circle's west edge; the case below wants it clear", far)
	}
	b.waiting = "forest"
	if !b.nearBoss("forest", far) {
		t.Error("a bot waiting the boss out went by where it saw it, not its circle")
	}
	b.waiting, b.mobs, b.boss = "", nil, 0
	b.mapID = "town"
	delete(b.bosses, "forest")
	b.bossUntil("cave", time.Now().Add(time.Minute))
	bats := w.spots("cave_bat")[0]
	if by, ok := b.bossBlocks(goal{kind: huntGoal, spot: bats}); !ok || by != "cave" {
		t.Errorf("the cave's bats, under its boss, are blocked by %q's, want the cave's", by)
	}
	if by, ok := b.bossBlocks(goal{kind: huntGoal, spot: cave}); ok {
		t.Errorf("the cave's boars, far from its boss, are blocked by %s's", by)
	}
	toForest := w.maps["town"].Portals[0]
	if at, ok := w.arrival(toForest); !ok || at != point(w.maps["forest"].Portals[0].At) {
		t.Errorf("through the town's portal a player comes out at %+v, want the forest's from_town", at)
	}
}
