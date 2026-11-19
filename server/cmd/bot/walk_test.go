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
	"finga/internal/shared/steer"
)

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
					if len(finder.Path(m.Grid, from.at, to.at, mv.Radius, from.at, leash)) == 0 {
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
	var walk steer.Walker
	var stuck steer.Stall
	var held sim.Input
	limit := time.Minute + time.Duration(2*nav.Distance(from, to)/mv.WalkSpeed*float64(time.Second))
	for took := time.Duration(0); took < limit; took += time.Duration(steps * mv.Dt * float64(time.Second)) {
		at := nav.Point{X: s.X, Z: s.Z}
		if nav.Distance(at, to) <= within {
			return took, true
		}
		held = walk.Keys(m, mv.Radius, at, to, took, stuck.Stalled(at, s.OnGround, held))
		for range steps {
			s = sim.Step(s, held, m.Geometry, mv)
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
