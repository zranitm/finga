package main

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
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
		walked := map[int]bool{}
		for i, sp := range m.Spawns {
			if k := m.AreaAt(sp.At.X, sp.At.Z); m.Kind == gamedata.Region && walked[k] { // a region's walks are long: one camp an area
				continue
			} else {
				walked[k] = true
			}
			s := spot{mapID: id, at: point(sp.At), radius: sp.Radius}
			targets = append(targets, target{fmt.Sprintf("a roam in spawn %d's %s", i, sp.Mob), b.roll(s), 1, true})
		}
		starts := []target{{name: "where players appear", at: point(m.PlayerSpawn)}}
		for _, p := range m.Portals {
			if m.Kind != gamedata.Region { // a region's walks are long, and its gates are reached from where players appear
				starts = append(starts, target{name: "portal " + p.Name, at: point(p.At)})
			}
		}
		for _, from := range starts {
			for _, to := range targets {
				t.Run(fmt.Sprintf("%s from %s to %s", id, from.name, to.name), func(t *testing.T) {
					t.Parallel() // each walk reads the map and its grid alone
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
	var walk steer.Walker
	var stuck steer.Stall
	var held sim.Input
	limit := time.Minute + time.Duration(2*nav.Distance(from, to)/mv.WalkSpeed*float64(time.Second))
	for took := time.Duration(0); took < limit; took += time.Duration(steps * mv.Dt * float64(time.Second)) {
		at := nav.Point{X: s.X, Z: s.Z}
		if nav.Distance(at, to) <= within {
			return took, true
		}
		held = walk.Keys(m, m.Grid(), mv.Radius, at, to, took, stuck.Stalled(at, s.OnGround, held))
		for range steps {
			s = sim.Step(s, held, m.Geometry, mv)
		}
	}
	return limit, false
}

// C12: with a field boss up, a bot hunts its map clear of where its blows could reach the boss; a beaten one, not yet back, keeps it from nothing.
func TestABotKeepsClearOfAFieldBoss(t *testing.T) {
	data := gamedatatest.Full(t)
	grove, _ := data.Map("shroom_grove")
	b := &bot{data: data, mapID: "shroom_grove", rng: rand.New(rand.NewPCG(1, 2))}
	at, r := point(grove.Boss.At), grove.Boss.Radius
	b.bossBackIn("shroom_grove", time.Minute)
	if b.nearBoss("shroom_grove", at) {
		t.Fatal("near a boss that isn't back yet")
	}
	b.bossBackIn("shroom_grove", 0)
	clear := bossClear
	for _, c := range []struct {
		name string
		seen bool
		at   nav.Point
		want bool
	}{
		{"in its reach", true, nav.Point{X: at.X - clear + 0.5, Z: at.Z}, true},
		{"past it", true, nav.Point{X: at.X - clear - 0.5, Z: at.Z}, false},
		{"on a diagonal, as the bot keeps a square clear", true, nav.Point{X: at.X - clear + 0.5, Z: at.Z + clear - 0.5}, true},
		{"unseen, in its circle's reach", false, nav.Point{X: at.X - r - clear + 0.5, Z: at.Z}, true},
		{"unseen, past its circle's reach", false, nav.Point{X: at.X - r - clear - 0.5, Z: at.Z}, false},
	} {
		b.mobs, b.boss = nil, 0
		if c.seen {
			b.mobs, b.boss = map[uint32]*clientv1.MobView{9: {Id: 9, Type: "spore_mother", X: at.X, Z: at.Z, Hp: 100}}, 9
		}
		if got := b.nearBoss("shroom_grove", c.at); got != c.want {
			t.Errorf("%s: near %v, want %v", c.name, got, c.want)
		}
	}
	b.mobs, b.boss = map[uint32]*clientv1.MobView{9: {Id: 9, Type: "spore_mother", X: at.X - r, Z: at.Z, Hp: 100}}, 9
	far := nav.Point{X: at.X - r + clear + 0.5, Z: at.Z} // past the boss's reach where it's seen, in its circle's
	if b.nearBoss("shroom_grove", far) {
		t.Errorf("%+v is near the boss seen at its circle's west edge, want it clear", far)
	}
}

// A hunt in Willowmere never rolls a point where a blow could reach an area's boss, and a gate brings a player out at the portal it names.
func TestABotKeepsOffWillowmeresBossesAndComesOutAtItsGates(t *testing.T) {
	data, w := testWorld(t)
	town := data.StartTown()
	sun := town.Areas[town.AreaAt(820, 130)]
	if sun.Boss == nil || sun.Boss.Mob != "king_slime" {
		t.Fatalf("Sunmeadow's boss is %+v, want the king slime", sun.Boss)
	}
	b := &bot{data: data, world: w, mapID: town.ID, rng: rand.New(rand.NewPCG(1, 2))}
	ring := spot{mapID: town.ID, at: point(sun.Boss.At), radius: sun.Boss.Radius + 2*bossClear} // a circle that takes in the boss's reach
	for range 200 {
		if p := b.roll(ring); b.nearBoss(town.ID, p) {
			t.Fatalf("rolled %+v, near the boss", p)
		}
	}
	i := slices.IndexFunc(town.Portals, func(p gamedata.Portal) bool { return p.Name == "cave_mouth" })
	cave := w.maps["greyfang_cave"]
	if i < 0 {
		t.Fatal("no cave_mouth gate")
	}
	if at, ok := w.arrival(town.Portals[i]); !ok || at != point(cave.Portals[slices.IndexFunc(cave.Portals, func(p gamedata.Portal) bool { return p.Name == "mouth" })].At) {
		t.Errorf("through the cave mouth a player comes out at %+v, want the cave's mouth", at)
	}
}

// A bot keeps far enough from a field boss that none of its blows can wake it: its prey, past bossClear, stands farther than the boss's body plus the widest area round the bot or its lock with the bot's own reach to its prey, or plus the longest shot that goes on past its first mob. A one-mob shot, as every basic attack is, stops at the prey it's aimed at.
func TestABotsBlowsNeverReachAFieldBoss(t *testing.T) {
	data, _ := testWorld(t)
	body, around, reach, shot := 0.0, 0.0, 0.0, 0.0
	for _, m := range data.Mobs() {
		if m.Boss {
			body = max(body, m.Radius)
		}
	}
	for _, s := range data.Skills() {
		for _, st := range s.Stages {
			switch sh := st.Shape; sh.Kind {
			case gamedata.AreaShape, gamedata.ArcShape:
				around = max(around, sh.Radius)
			case gamedata.ShotShape:
				if st.Targets != 1 { // several mobs, or a status skill's every mob in it
					shot = max(shot, sh.Range+sh.Width/2)
				}
			}
		}
	}
	for _, c := range data.Classes() {
		if s := c.Attack.Shape; s.Kind != gamedata.ShotShape {
			reach = max(reach, s.Radius)
		}
	}
	if need := body + max(around+reach, shot); bossClear <= need {
		t.Errorf("bossClear %v m, want past %v: the boss's body %v, then the widest area %v and a bot's reach %v, or the longest shot past its first mob %v", bossClear, need, body, around, reach, shot)
	}
}
