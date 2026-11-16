package mobs_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"finga/internal/application/world/mobs"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// boarReach is how far apart a wild boar's centre and a player's may be for its bite: their radii and the bite's 0.75 m.
const boarReach = 0.875 + playerRadius + 0.75

// boarAt is one wild boar standing at (x, z) on g, facing north, its circle a hair wide.
func boarAt(t *testing.T, g sim.Geometry, x, z float64) *mobs.Arena {
	t.Helper()
	return newArena(t, meadow(t, g, spawn("wild_boar", 1, x, z, 0.01)), worldtest.FixedRoll{V: 0.5})
}

// bitten is whether a landing this tick hit id.
func bitten(a *mobs.Arena, id kernel.CharacterID) bool {
	for _, l := range a.Landings() {
		for _, h := range l.Hits {
			if h.Player == id {
				return true
			}
		}
	}
	return false
}

// up is p with its feet dy over the ground.
func up(p mobs.Body, dy float64) mobs.Body {
	p.Y = dy
	return p
}

// A mob comes after a player in its sight, 9.375 m either way and from 1 m below its feet to 3 m above, and leaves the rest be.
func TestAMobComesAfterOnlyAPlayerItSees(t *testing.T) {
	for _, tt := range []struct {
		name  string
		p     mobs.Body
		comes bool
	}{
		{"8 m off", player(20, 12), true},
		{"10 m off", player(20, 10), false},
		{"8 m off, 2.5 m up", up(player(20, 12), 2.5), true},
		{"8 m off, 3.5 m up", up(player(20, 12), 3.5), false},
		{"8 m off, 1.5 m down", up(player(20, 12), -1.5), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := alone(t, "wild_boar")
			home := a.Views()[0]
			for n := range uint64(100) {
				a.Step(n+1, []mobs.Body{tt.p})
				if v := a.Views()[0]; !tt.comes && distance(v, home.X, home.Z) > 0.5 {
					t.Fatalf("tick %d: the boar at (%.2f, %.2f) left its spot for a player it can't see", n+1, v.X, v.Z)
				}
			}
			if v := a.Views()[0]; tt.comes && distance(v, tt.p.X, tt.p.Z) > boarReach {
				t.Fatalf("the boar at (%.2f, %.2f) after 5 s; want it at the player's side", v.X, v.Z)
			}
		})
	}
}

// boxGap is how far (x, z) is from an unturned box's sides.
func boxGap(b sim.Box, x, z float64) float64 {
	return math.Hypot(max(math.Abs(x-b.X)-b.HalfW, 0), max(math.Abs(z-b.Z)-b.HalfD, 0))
}

// A mob walks round a wall between it and its target, never into it, and bites them.
func TestAMobWalksRoundAWallToItsTarget(t *testing.T) {
	wall := sim.Box{X: 20, Z: 22.5, HalfW: 4, HalfD: 0.5, UX: 1, Top: 3}
	a := boarAt(t, open(wall), 20, 20)
	p := player(20, 25.5)
	for n := range uint64(200) {
		a.Step(n+1, []mobs.Body{p})
		if v := a.Views()[0]; boxGap(wall, v.X, v.Z) < 0.875-1e-9 {
			t.Fatalf("tick %d: the boar at (%.3f, %.3f) is in the wall", n+1, v.X, v.Z)
		}
		if bitten(a, p.ID) {
			return
		}
	}
	v := a.Views()[0]
	t.Fatalf("the boar at (%.2f, %.2f) never bit the player behind the wall in 10 s", v.X, v.Z)
}

// A mob wedged between a wall and a tree, its cell too tight for it, walks out and round them to its target.
func TestAMobWedgedInACornerGetsFree(t *testing.T) {
	g := open(sim.Box{X: 18, Z: 22.5, HalfW: 4, HalfD: 0.5, UX: 1, Top: 4})
	g.Cylinders = []sim.Cylinder{{X: 23.71, Z: 20.61, Radius: 0.6, Top: 4}}
	wedged := nav.Point{X: 22.3, Z: 22 - 0.875} // touching the wall's south face and the tree
	m := meadow(t, g, spawn("wild_boar", 1, wedged.X, wedged.Z, 0.01))
	if m.Grid().Fits(wedged, 0.875) {
		t.Fatal("the boar's cell fits it; want one too tight at its centre")
	}
	a := newArena(t, m, worldtest.FixedRoll{V: 0.5})
	p := player(24, 25) // past the gap between the wall's end and the tree, too narrow for the boar
	for n := range uint64(200) {
		a.Step(n+1, []mobs.Body{p})
		if bitten(a, p.ID) {
			return
		}
	}
	v := a.Views()[0]
	t.Fatalf("the boar at (%.2f, %.2f) never got to the player in 10 s", v.X, v.Z)
}

// A mob follows a player only to its leash, 16 m past its circle, then walks home blind to them.
func TestAMobGivesUpOnAPlayerPastItsLeash(t *testing.T) {
	a := boarAt(t, open(), 20, 6)
	home := a.Views()[0]
	leash := 0.01 + worldtest.LoadData(t).MobRules().Leash
	p, farthest := player(20, 9), 0.0
	for n := range uint64(400) {
		p.Z = min(9+float64(3*float64(n)*tick.Seconds()), 36) // running north at 3 m/s, faster than the boar
		a.Step(n+1, []mobs.Body{p})
		v := a.Views()[0]
		if distance(v, home.X, home.Z) > leash {
			t.Fatalf("tick %d: the boar at (%.2f, %.2f), past its leash", n+1, v.X, v.Z)
		}
		farthest = max(farthest, distance(v, home.X, home.Z))
	}
	if farthest < 5 {
		t.Fatalf("the boar went at most %.2f m from home; want it to have chased the player first", farthest)
	}
	if v := a.Views()[0]; distance(v, home.X, home.Z) > 0.5 {
		t.Fatalf("the boar at (%.2f, %.2f) 20 s on; want it home at (%.2f, %.2f)", v.X, v.Z, home.X, home.Z)
	}
}

// ring is which contact ring about a player d metres off a mob of radius r stands on, -1 for none.
func ring(d, r float64) int {
	for k := range 4 {
		if math.Abs(d-(playerRadius+r+0.05+float64(k)*2*r)) < 1e-6 {
			return k
		}
	}
	return -1
}

// crowd is where a crowd of mobs about a player ended: those whose bite reaches, and those waiting on the outer rings.
type crowd struct{ biting, waiting []uint64 }

// settle runs 20 s from tick from and fails unless its mobs of radius r end still, no two overlapping, each where its bite of reach reaches or on an outer ring.
func settle(t *testing.T, a *mobs.Arena, p mobs.Body, r, reach float64, from uint64) crowd {
	t.Helper()
	var before []model.MobView
	for n := from; n < from+400; n++ {
		a.Step(n, []mobs.Body{p})
		if n == from+380 {
			before = a.Views()
		}
	}
	var c crowd
	views := a.Views()
	for i, v := range views {
		d := distance(v, p.X, p.Z)
		switch {
		case v.X != before[i].X || v.Z != before[i].Z:
			t.Fatalf("mob %d at (%.3f, %.3f) still moving", v.ID, v.X, v.Z)
		case d <= reach+1e-9:
			c.biting = append(c.biting, v.ID)
		case ring(d, r) >= 1:
			c.waiting = append(c.waiting, v.ID)
		default:
			t.Fatalf("mob %d at (%.3f, %.3f), %.3f m off the player: neither in reach nor on a ring", v.ID, v.X, v.Z, d)
		}
		for _, w := range views[i+1:] {
			if d := distance(v, w.X, w.Z); d < 2*r-1e-6 {
				t.Fatalf("mobs at (%.3f, %.3f) and (%.3f, %.3f) overlap, %.3f m apart", v.X, v.Z, w.X, w.Z, d)
			}
		}
	}
	return c
}

// Six mobs chasing one player end on spots of their own about them, those who can't reach them waiting on an outer ring, solids or none.
func TestChasersEndOnSpotsOfTheirOwn(t *testing.T) {
	wall := sim.Box{X: 21.6, Z: 20, HalfW: 0.5, HalfD: 2, UX: 1, Top: 3} // a wall 1.1 m east of the player
	for _, seed := range [][]float64{{0.1, 0.7, 0.4, 0.9, 0.25}, {0.3, 0.8, 0.15, 0.6}, {0.55, 0.05, 0.95, 0.35, 0.75}} {
		for _, g := range []sim.Geometry{open(), open(wall)} {
			t.Run(fmt.Sprintf("%v, %d solids", seed, len(g.Boxes)), func(t *testing.T) {
				a := newArena(t, meadow(t, g, spawn("wild_boar", 6, 17, 14, 3)), &cycle{v: seed})
				if c := settle(t, a, player(20, 20), 0.875, boarReach, 1); len(c.biting) < 2 || len(c.waiting) == 0 {
					t.Fatalf("%d biting, %d waiting; want the player's side full and the rest waiting", len(c.biting), len(c.waiting))
				}
			})
		}
	}
}

// biting is the game data with the green slime biting, so its attack moves it off no spot.
func biting(t *testing.T) *gamedata.Data {
	t.Helper()
	return worldtest.LoadDataWith(t, map[string]func(string) string{
		"mobs.yaml": func(s string) string { return strings.Replace(s, "attacks: [{use: hop}]", "attacks: [{use: bite}]", 1) },
	})
}

// Ten slimes on one player fill the room at their side and wait on an outer ring; when one at their side dies, a waiting one moves in.
func TestAWaitingChaserMovesInWhenOneAtThePlayersSideDies(t *testing.T) {
	slimes := spawn("green_slime", 10, 20, 14, 3)
	slimes.Respawn = time.Hour // the dead one stays dead
	a := mobs.NewArena(mobs.Deps{Map: meadow(t, open(), slimes), Data: biting(t), Roll: &cycle{v: []float64{0.1, 0.7, 0.4, 0.9, 0.25}}}, mobs.Tuning{TickInterval: tick})
	p := player(20, 20)
	const reach = 0.5 + playerRadius + 0.75
	c := settle(t, a, p, 0.5, reach, 1)
	if len(c.biting) < 4 || len(c.waiting) == 0 {
		t.Fatalf("%d biting, %d waiting; want the player's side full and the rest waiting", len(c.biting), len(c.waiting))
	}
	var inner model.MobView
	for _, v := range a.Views() {
		if v.ID == c.biting[0] {
			inner = v
		}
	}
	kill := mobs.Strike{By: kernel.NewCharacterID(), Shape: gamedata.Shape{Kind: gamedata.AreaShape, Radius: 0.01}, Most: 1, Power: strong, Aim: model.Aim{X: inner.X, Z: inner.Z}}
	if blow, _ := one(t, a.Strike(kill)); blow.Killed == nil || blow.Damage.Target.Mob != inner.ID {
		t.Fatalf("blow %+v; want the slime %d killed", blow, inner.ID)
	}
	if after := settle(t, a, p, 0.5, reach, 401); len(after.biting) != len(c.biting) || len(after.waiting) != len(c.waiting)-1 {
		t.Fatalf("%d biting, %d waiting after one died, from %d and %d; want a waiting one moved in", len(after.biting), len(after.waiting), len(c.biting), len(c.waiting))
	}
}

// A mob never steps into a portal, even with its target standing in one, so the player can always leave by it.
func TestAMobNeverStepsIntoAPortal(t *testing.T) {
	m := meadow(t, open(), spawn("wild_boar", 1, 20, 20, 0.01))
	m.Portals = []gamedata.Portal{{Name: "out", At: gamedata.Point{X: 20, Z: 26}, Radius: 1, TargetMap: "town", TargetPortal: "to_meadow"}}
	a := newArena(t, m, worldtest.FixedRoll{V: 0.5})
	p := player(20, 26)
	bit := false
	for n := range uint64(200) {
		a.Step(n+1, []mobs.Body{p})
		if v := a.Views()[0]; distance(v, 20, 26) < 1+0.875-1e-9 {
			t.Fatalf("tick %d: the boar at (%.3f, %.3f) stepped into the portal", n+1, v.X, v.Z)
		}
		bit = bit || bitten(a, p.ID)
	}
	if !bit {
		t.Fatal("the boar never bit the player from the portal's edge")
	}
}

// firstWindups is the tick of each mob's first windup.
func firstWindups(started []model.MobAttacked) map[uint64]uint64 {
	first := map[uint64]uint64{}
	for _, s := range started {
		if _, ok := first[s.Mob]; !ok {
			first[s.Mob] = s.Tick
		}
	}
	return first
}

// wait is the engage wait at a roll of 0.5: 200 ms of the rules' 0 to 400, in ticks.
const wait = 4

// A mob with a player come into its reach, after nobody was, starts its windup after a wait rolled from none to 400 ms.
func TestTheEngageWaitIsRolled(t *testing.T) {
	for _, tt := range []struct {
		roll float64
		at   uint64
	}{{0, 1}, {0.5, 1 + wait}, {0.999, 9}} {
		a := newArena(t, meadow(t, open(), spawn("wild_boar", 1, 20, 20, 0.01)), worldtest.FixedRoll{V: tt.roll})
		v := a.Views()[0]
		p := player(v.X, v.Z-1.3) // at its side, in its bite's reach
		if got := stepThrough(a, 1, 12, p).started; len(got) != 1 || got[0].Tick != tt.at {
			t.Fatalf("at a roll of %v: windups %+v; want the bite at tick %d", tt.roll, got, tt.at)
		}
	}
}

// twoBoars is a boar either side of (20, 20), 1.3 m off: a player there is at both their sides. Its rolls cycle through three waits, so rolls one or two apart differ.
func twoBoars(t *testing.T) *mobs.Arena {
	t.Helper()
	m := meadow(t, open(), spawn("wild_boar", 1, 18.7, 20, 0.01), spawn("wild_boar", 1, 21.3, 20, 0.01))
	return newArena(t, m, &cycle{v: []float64{0.1, 0.5, 0.9}}) // waits of 1, 4 and 8 ticks
}

// Mobs that see a player on one tick don't attack as one: each rolls its own wait.
func TestMobsThatSeeAPlayerTogetherWindUpApart(t *testing.T) {
	a := twoBoars(t)
	v := a.Views()
	first := firstWindups(stepThrough(a, 1, 12, player(20, 20)).started)
	if len(first) != 2 || first[v[0].ID] == first[v[1].ID] {
		t.Fatalf("first windups by mob %v; want both boars winding up, at different ticks", first)
	}
}

// Mobs whose target, seen but out of reach, comes into reach of them all on one tick each roll their own wait.
func TestAPileComingIntoReachTogetherWindsUpApart(t *testing.T) {
	a := twoBoars(t)
	v := a.Views()
	p := player(20, 20)
	if got := stepThrough(a, 1, 80, up(p, 2.5)).started; len(got) != 0 { // seen, 2.5 m up and out of reach: they stand under them
		t.Fatalf("windups %+v; want none at a player out of reach above", got)
	}
	first := firstWindups(stepThrough(a, 81, 100, p).started) // down among them at tick 81
	if len(first) != 2 || first[v[0].ID] == first[v[1].ID] {
		t.Fatalf("first windups by mob %v; want both boars winding up, at different ticks", first)
	}
}

// A mob waits only when someone comes into its reach after nobody was; switching to another player in reach adds no wait.
func TestASwitchOfTargetInReachHasNoWait(t *testing.T) {
	a := boarAt(t, open(), 20, 20)
	v := a.Views()[0]
	first, second := player(v.X, v.Z-1.3), player(v.X+1.3, v.Z) // both at its side; first, found first, is its target
	got := stepThrough(a, 1, 2, first, second).started
	got = append(got, stepThrough(a, 3, 12, second).started...) // first gone at tick 3: it switches to second, still in reach
	if len(got) != 1 || got[0].Tick != 1+wait || math.Abs(float64(got[0].Yaw)-math.Pi/2) > 1e-3 {
		t.Fatalf("windups %+v; want the bite at second, east, at tick %d: the wait from tick 1, none rolled again at the switch", got, 1+wait)
	}
}

// A mob whose target leaves its reach, as a knockback carries them, and comes back waits again before its next windup.
func TestAMobWaitsAgainWhenItsTargetComesBackIntoReach(t *testing.T) {
	a := boarAt(t, open(), 20, 20)
	v := a.Views()[0]
	p := player(v.X, v.Z-1.3)
	ticks := stepThrough(a, 1, 20, p).startTicks()
	ticks = append(ticks, stepThrough(a, 21, 60, up(p, 2.5)).startTicks()...) // out of its reach above it, still in sight
	ticks = append(ticks, stepThrough(a, 61, 70, p).startTicks()...)
	if want := []uint64{1 + wait, 61 + wait}; !slices.Equal(ticks, want) {
		t.Fatalf("windups at ticks %v; want %v: its bite ready from tick %d, but a wait rolled again when the player came back at 61", ticks, want, 1+wait+40)
	}
}

// A leap stops at a solid in its way, as the sim stops a body, and lands on nobody past it.
func TestALeapStopsAtABlock(t *testing.T) {
	wall := sim.Box{X: 20, Z: 18.5, HalfW: 3, HalfD: 0.25, UX: 1, Top: 3}
	a := newArena(t, meadow(t, open(wall), spawn("green_slime", 1, 20, 20, 0.01)), worldtest.FixedRoll{V: 0.5})
	p := player(20, 17) // past the wall, where the hop would land touching them
	at, next := windup(t, a, []mobs.Body{p}, 1)
	if math.Abs(at.ToZ-(18.75+0.5)) > 1e-9 {
		t.Fatalf("windup %+v; want its landing at the wall's face, z %v", at, 18.75+0.5)
	}
	if hits := landed(a, []mobs.Body{p}, next, 20); len(hits) != 0 {
		t.Fatalf("hits %+v; want none past the wall", hits)
	}
	if v := a.Views()[0]; math.Abs(v.Z-at.ToZ) > 1e-9 {
		t.Fatalf("the slime landed at z %v; want %v", v.Z, at.ToZ)
	}
}

// A boss keeps out of portals too: with its target on one, it stands at the portal's edge, facing them, and its attacks reach across.
func TestABossHoldsAtAPortalsEdge(t *testing.T) {
	m := meadow(t, open())
	m.Portals = []gamedata.Portal{{Name: "out", At: gamedata.Point{X: 30, Z: 36}, Radius: 1, TargetMap: "town", TargetPortal: "to_meadow"}}
	a := newArena(t, m, worldtest.FixedRoll{V: 0.5})
	def, spot := king(t)
	a.SpawnBoss(def, spot)
	p := player(30, 36)
	hit := false
	for n := range uint64(300) {
		a.Step(n+1, []mobs.Body{p})
		if v := a.Views()[0]; distance(v, 30, 36) < 1+def.Radius-1e-9 {
			t.Fatalf("tick %d: the king at (%.3f, %.3f) stepped into the portal", n+1, v.X, v.Z)
		}
		hit = hit || bitten(a, p.ID)
	}
	if v := a.Views()[0]; !hit || distance(v, 30, 36) > 1+def.Radius+0.05 || math.Abs(float64(v.Yaw)) > 1e-3 {
		t.Fatalf("king %+v, hit %v; want it at the portal's edge, facing north to the player, having hit them", v, hit)
	}
}

// No mob or boss on a shipped map steps into a portal: provoked by a player out of reach over each, shooters too come to the portals' edges, never into one, and some attack across one.
func TestNoShippedMobStepsIntoAPortal(t *testing.T) {
	data := worldtest.LoadData(t)
	hits := 0 // landings on a player standing on a portal, over every map
	for _, m := range data.Maps() {
		if m.Kind != gamedata.Hunting || len(m.Portals) == 0 {
			continue
		}
		a := mobs.NewArena(mobs.Deps{Map: m, Data: data, Roll: rand.New(rand.NewPCG(1, 2))}, mobs.Tuning{TickInterval: tick})
		if m.Boss != nil {
			def, _ := data.Mob(m.Boss.Mob)
			a.SpawnBoss(def, *m.Boss)
		}
		var players []mobs.Body // a player 2.5 m over each portal, then one on it, whom a mob waiting at the edge attacks if it reaches
		for _, q := range m.Portals {
			h := m.Geometry.Terrain.Height(q.At.X, q.At.Z)
			players = append(players, up(player(q.At.X, q.At.Z), h+2.5), up(player(q.At.X, q.At.Z), h))
		}
		closest := math.Inf(1)       // the least gap between a mob and a portal's edge
		for n := range uint64(400) { // 20 s: every circle keeps out of sight of the portals, and the forest's slimes walk 1 m/s
			a.Step(n+1, players)
			for k := range m.Portals {
				if bitten(a, players[2*k+1].ID) {
					hits++
				}
			}
			if n%100 == 0 { // a provoke lasts 5 s
				for k, q := range m.Portals {
					a.Strike(mobs.Strike{By: players[2*k].ID, Shape: gamedata.Shape{Kind: gamedata.AreaShape, Radius: 30}, Most: 100, Power: model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 1}, Aim: model.Aim{X: q.At.X, Z: q.At.Z}})
				}
			}
			for _, v := range a.Views() {
				def, _ := data.Mob(v.Type)
				for _, q := range m.Portals {
					gap := distance(v, q.At.X, q.At.Z) - q.Radius - def.Radius
					if gap < -1e-9 {
						t.Fatalf("%s tick %d: %s at (%.2f, %.2f) in portal %s", m.ID, n+1, v.Type, v.X, v.Z, q.Name)
					}
					closest = min(closest, gap)
				}
			}
		}
		if closest > 0.5 {
			t.Errorf("%s: no mob came within 0.5 m of a portal's edge in 20 s, %.2f m at the closest", m.ID, closest)
		}
	}
	if hits == 0 {
		t.Error("no mob hit a player standing on a portal; want those that reach across the edge to")
	}
}

// On a map nobody is on, a windup under way plays out on nobody and no mob wanders; a player come later meets a fresh wait.
func TestAMapNobodyIsOnRests(t *testing.T) {
	a := newArena(t, meadow(t, open(), spawn("wild_boar", 1, 20, 20, 3)), worldtest.FixedRoll{V: 0.5})
	v := a.Views()[0]
	p := player(v.X, v.Z-1.3)
	_, next := windup(t, a, []mobs.Body{p}, 1)
	for n := next; n < next+200; n++ {
		a.Rest(n)
		if len(a.Landings()) != 0 {
			t.Fatalf("tick %d: landings %+v on a map nobody is on", n, a.Landings())
		}
	}
	if w := a.Views()[0]; w.X != v.X || w.Z != v.Z {
		t.Fatalf("the boar at (%.3f, %.3f) after 10 s alone; want it resting at (%.3f, %.3f)", w.X, w.Z, v.X, v.Z)
	}
	if got := stepThrough(a, next+200, next+220, p).started; len(got) != 1 || got[0].Tick != next+200+wait {
		t.Fatalf("windups %+v; want one after the wait, at tick %d", got, next+200+wait)
	}
}
