package bots_test

import (
	"math"
	"testing"

	"finga/internal/application/world/bots"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// newBattleFight is a bot of side 1 on the shipped battlefield at (x, z), walking its side's grid, its enemy in their own base.
func newBattleFight(t *testing.T, class kernel.Class, x, z float64) *fight {
	t.Helper()
	f := newFight(t, class, sure())
	m, _ := f.data.Map("battlefield")
	f.setup.Map, f.setup.Grid = m, m.KeptGrid(0)
	f.brain = bots.New(f.setup)
	f.moveTo(x, z)
	f.it.Body.X, f.it.Body.Z = m.Bases[1].At.X, m.Bases[1].At.Z
	return f
}

func (f *fight) moveTo(x, z float64) {
	f.self.State.X, f.self.State.Z, f.me.Body.X, f.me.Body.Z = x, z, x, z
}

// battle is the battlefield's points, west, middle and east, held by holders, with n bots on side 1.
func (f *fight) battle(n int, holders ...int) *bots.Battle {
	m, _ := f.data.Map("battlefield")
	b := &bots.Battle{Bots: n}
	for k, pt := range m.Points {
		b.Points = append(b.Points, bots.Point{Circle: pt.Circle, Holder: holders[k]})
	}
	return b
}

// decideBattle records the bot and seen on every tick up to at, then has it decide at it in battle, team its side's.
func (f *fight) decideBattle(team *bots.Team, battle *bots.Battle, at uint64, seen ...bots.Seen) bots.Decision {
	for k := at - min(at, 19); k <= at; k++ {
		f.ring.Record(k, append([]bots.Seen{f.me}, seen...))
	}
	return f.brain.Decide(bots.View{Tick: at, Fighting: true, Self: f.self, Ring: f.ring, Team: team, Battle: battle, Holds: f.holds})
}

// travel has the bot decide every other tick from at, moving it a quarter metre along its keys each time, until it holds still; it gives the tick it stopped at, or fails past 1,000 decisions.
func (f *fight) travel(team *bots.Team, battle *bots.Battle, at uint64, seen ...bots.Seen) uint64 {
	f.t.Helper()
	for range 1000 {
		k := f.decideBattle(team, battle, at, seen...).Keys
		dx, dz := axis(k.Right, k.Left), axis(k.Up, k.Down)
		if dx == 0 && dz == 0 {
			return at
		}
		l := math.Hypot(dx, dz)
		f.moveTo(f.self.State.X+0.25*dx/l, f.self.State.Z+0.25*dz/l)
		at += 2
	}
	f.t.Fatalf("the bot never stopped; it's at (%.1f, %.1f)", f.self.State.X, f.self.State.Z)
	return at
}

func axis(plus, minus bool) float64 {
	switch {
	case plus && !minus:
		return 1
	case minus && !plus:
		return -1
	}
	return 0
}

// staysIn is whether the bot, travelling from at, stops in point k and holds still there five decisions more, picking no new spot.
func (f *fight) staysIn(k int, team *bots.Team, b *bots.Battle, at uint64, seen ...bots.Seen) bool {
	at = f.travel(team, b, at, seen...)
	for range 5 {
		at += 2
		if f.decideBattle(team, b, at, seen...).Keys != (sim.Input{}) {
			return false
		}
	}
	return f.standsIn(b) == k
}

// standsIn is the point the bot stands in, -1 for none; within half its radius from its middle, give or take where it stops.
func (f *fight) standsIn(b *bots.Battle) int {
	for k, pt := range b.Points {
		if math.Hypot(f.self.State.X-pt.At.X, f.self.State.Z-pt.At.Z) <= pt.Radius/2+0.5 {
			return k
		}
	}
	return -1
}

const west, middle, east = 0, 1, 2

// pvp.md section 8: in a battle a bot goes to the nearest point its side doesn't hold, else the nearest contested one, else the nearest, to guard it, and stands in it.
func TestABattleBotGoesToTheNearestPointItsSideDoesntHold(t *testing.T) {
	for _, c := range []struct {
		name      string
		holders   []int
		contested int // -1 for none
		want      int
	}{
		{"none held", []int{0, 0, 0}, -1, west},
		{"the enemy's nearest", []int{2, 2, 2}, -1, west},
		{"its own west", []int{1, 0, 2}, -1, middle},
		{"its own west, the enemy's middle", []int{1, 2, 0}, -1, middle},
		{"its own west contested, the middle no one's", []int{1, 0, 2}, west, middle},
		{"its own west and middle", []int{1, 1, 0}, -1, east},
		{"all its own, the east contested", []int{1, 1, 1}, east, east},
		{"all its own, none contested", []int{1, 1, 1}, -1, west},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newBattleFight(t, "warrior", 26, 45)
			b := f.battle(15, c.holders...)
			if c.contested >= 0 {
				b.Points[c.contested].Contested = true
			}
			f.travel(&bots.Team{}, b, 20, f.it)
			if got := f.standsIn(b); got != c.want {
				t.Fatalf("held by %v, contested %d: the bot stopped at (%.1f, %.1f), in point %d; want %d", c.holders, c.contested, f.self.State.X, f.self.State.Z, got, c.want)
			}
		})
	}
}

// pvp.md section 8: no point takes more than half its side's bots, one at least; the next goes on to the next nearest.
func TestNoPointTakesMoreThanHalfTheSidesBots(t *testing.T) {
	for _, c := range []struct {
		bots, first int // the side's bots, and how many go to the west point before the last
	}{{4, 2}, {3, 1}, {1, 0}} {
		team := &bots.Team{}
		var last *fight
		var b *bots.Battle
		for k := range c.first + 1 {
			f := newBattleFight(t, "warrior", 26, 45)
			b = f.battle(c.bots, 0, 0, 0)
			f.decideBattle(team, b, 20+uint64(k))
			last = f
		}
		last.travel(team, b, 30, last.it)
		want := west
		if c.first == max(1, c.bots/2) {
			want = middle
		}
		if got := last.standsIn(b); got != want {
			t.Fatalf("%d bots a side, %d gone to the west point: the last stopped in point %d; want %d", c.bots, c.first, got, want)
		}
	}
}

// With every point at its half of the side's bots, as on a map of one point, a bot still goes to the nearest.
func TestABotStillGoesToAPointWhenEveryOneIsFull(t *testing.T) {
	team := &bots.Team{}
	var f *fight
	var b *bots.Battle
	for k := range 3 {
		f = newBattleFight(t, "warrior", 26, 45)
		b = f.battle(4, 0, 0, 0)
		b.Points = b.Points[:1]
		if k < 2 {
			f.decideBattle(team, b, 20+uint64(k))
		}
	}
	f.travel(team, b, 30, f.it)
	if got := f.standsIn(b); got != west {
		t.Fatalf("the one point full: the third bot stopped at (%.1f, %.1f), in point %d; want it in the point all the same", f.self.State.X, f.self.State.Z, got)
	}
}

// pvp.md section 8: in a point its side holds, uncontested and with no enemy near, two of the side's bots there stay, the first by id, and the rest go on to a point that needs them; none goes on while the point is contested or every other is held.
func TestTwoStayAtAHeldPointAndTheRestGoOn(t *testing.T) {
	m, _ := newFight(t, "warrior", sure()).data.Map("battlefield")
	at := m.Points[west].At
	setUp := func(holders ...int) (*bots.Team, *bots.Battle, []*fight) {
		team := &bots.Team{}
		var fs []*fight
		var b *bots.Battle
		for k := range 3 {
			f := newBattleFight(t, "warrior", 26, 45)
			b = f.battle(9, 0, holders[1], holders[2])
			f.decideBattle(team, b, 20+uint64(k)) // each takes the west point, none holding it
			f.moveTo(at.X+float64(k-1), at.Z)
			fs = append(fs, f)
		}
		b.Points[west].Holder = 1
		return team, b, fs
	}
	ids := func(fs []*fight) []bots.Seen {
		var out []bots.Seen
		for _, f := range fs {
			out = append(out, f.me)
		}
		return out
	}
	last := func(fs []*fight) (*fight, []*fight) { // the bot with the highest id, and the others
		k := 0
		for j := range fs {
			if fs[j].me.ID.String() > fs[k].me.ID.String() {
				k = j
			}
		}
		var rest []*fight
		for j := range fs {
			if j != k {
				rest = append(rest, fs[j])
			}
		}
		return fs[k], rest
	}

	team, b, fs := setUp(0, 0, 0)
	goer, stayers := last(fs)
	for k, f := range stayers {
		if !f.staysIn(west, team, b, 40, append(ids(fs), f.it)...) {
			t.Fatalf("stayer %d, its id before the last's, stopped in point %d or kept moving; want it holding still in the held west point", k, f.standsIn(b))
		}
	}
	goer.travel(team, b, 40, append(ids(stayers), goer.it)...)
	if got := goer.standsIn(b); got != middle {
		t.Fatalf("the third of the side's bots in the held west point stopped in point %d; want it gone on to the middle", got)
	}

	team, b, fs = setUp(0, 0, 0)
	b.Points[west].Holder = 0 // still turning
	goer, stayers = last(fs)
	if !goer.staysIn(west, team, b, 40, append(ids(stayers), goer.it)...) {
		t.Fatalf("the west point no one's yet: the third bot stopped in point %d or kept moving; want it holding still to turn it", goer.standsIn(b))
	}

	team, b, fs = setUp(0, 0, 0)
	goer, stayers = last(fs)
	goer.moveTo(at.X+7, at.Z) // on its way in, between the west point and the middle
	if d := goer.decideBattle(team, b, 40, append(ids(stayers), goer.it)...); !d.Keys.Left || d.Keys.Right {
		t.Fatalf("the third bot not in the west point yet: keys %+v; want it walking west into it before going on", d.Keys)
	}

	team, b, fs = setUp(0, 0, 0)
	goer, stayers = last(fs)
	for _, f := range stayers {
		f.moveTo(26, 45) // still on their way
	}
	if !goer.staysIn(west, team, b, 40, append(ids(stayers), goer.it)...) {
		t.Fatalf("the two ahead of it not in the west point yet: the third bot stopped in point %d or kept moving; want it holding still till they're in", goer.standsIn(b))
	}

	team, b, fs = setUp(0, 0, 0)
	goer, stayers = last(fs)
	seen := ids(stayers)
	seen[0].Up = false
	if !goer.staysIn(west, team, b, 40, append(seen, goer.it)...) {
		t.Fatalf("one of the two ahead of it down: the third bot stopped in point %d or kept moving; want it holding still", goer.standsIn(b))
	}

	team, b, fs = setUp(0, 0, 1)
	fourth := newBattleFight(t, "warrior", 60, 45)
	fourth.decideBattle(team, b, 30) // it takes the middle, the one point its side needs
	b.Bots = 3                       // a point each, so the middle is full
	goer, stayers = last(fs)
	if !goer.staysIn(west, team, b, 40, append(ids(stayers), goer.it, fourth.me)...) {
		t.Fatalf("the one point that needs its side full: the third bot stopped in point %d or kept moving; want it holding still in the west", goer.standsIn(b))
	}

	team, b, fs = setUp(0, 1, 1)
	goer, stayers = last(fs)
	if !goer.staysIn(west, team, b, 40, append(ids(stayers), goer.it)...) {
		t.Fatalf("every point its side's: the third bot stopped in point %d or kept moving; want it holding still in the west", goer.standsIn(b))
	}

	team, b, fs = setUp(0, 0, 0)
	b.Points[west].Contested = true
	goer, stayers = last(fs)
	if !goer.staysIn(west, team, b, 40, append(ids(stayers), goer.it)...) {
		t.Fatalf("the west point contested: the third bot stopped in point %d or kept moving; want it holding still", goer.standsIn(b))
	}
}

// pvp.md section 8: a bot drops its point when it goes down and picks one afresh when it's up, from where the points stand then.
func TestABattleBotPicksItsPointAfreshAfterEachRespawn(t *testing.T) {
	f := newBattleFight(t, "warrior", 26, 45)
	team, b := &bots.Team{}, f.battle(15, 0, 0, 0)
	f.decideBattle(team, b, 20, f.it) // it takes the west point
	f.self.Down = true
	f.decideBattle(team, b, 22, f.it)
	f.self.Down = false
	b.Points[west].Holder = 1 // its side took it meanwhile
	f.travel(team, b, 24, f.it)
	if got := f.standsIn(b); got != middle {
		t.Fatalf("up again with the west point its side's: the bot stopped in point %d; want the middle", got)
	}
}

// pvp.md section 8: an enemy the bot could reach within 10 m draws it from its point to fight; one in their own base, which its side can't enter, never does, and it isn't fought.
func TestABattleBotFightsAnEnemyNearAndNotOneInTheirBase(t *testing.T) {
	f := newBattleFight(t, "warrior", 50, 45)
	b := f.battle(15, 0, 0, 0)
	f.it.Body.X, f.it.Body.Z = 50, 45+9
	d := f.decideBattle(&bots.Team{}, b, 20, f.it)
	if !d.Keys.Up || d.Keys.Left || d.Keys.Right {
		t.Fatalf("an enemy 9 m north: keys %+v; want the bot walking north to fight them", d.Keys)
	}
	f = newBattleFight(t, "warrior", 50, 45)
	f.it.Body.X, f.it.Body.Z = 50, 45+11
	if f.travel(&bots.Team{}, b, 20, f.it); f.standsIn(b) != west {
		t.Fatalf("an enemy 11 m north: the bot stopped at (%.1f, %.1f); want it gone to the west point", f.self.State.X, f.self.State.Z)
	}
	f = newBattleFight(t, "warrior", 50, 45)
	f.it.Body.X, f.it.Body.Z, f.it.Up = 50, 45+5, false
	if f.travel(&bots.Team{}, b, 20, f.it); f.standsIn(b) != west {
		t.Fatalf("an enemy down 5 m north: the bot stopped at (%.1f, %.1f); want it gone to the west point", f.self.State.X, f.self.State.Z)
	}
	m, _ := f.data.Map("battlefield")
	base := m.Bases[1]
	f = newBattleFight(t, "warrior", base.At.X-base.Radius-1.5, base.At.Z)
	f.it.Body.X, f.it.Body.Z = base.At.X-base.Radius+1, base.At.Z // 2.5 m off, in their base
	if d := f.decideBattle(&bots.Team{}, b, 20, f.it); d.Attack != nil || d.Skill != nil || !d.Keys.Left {
		t.Fatalf("an enemy 2.5 m off in their own base: %+v; want the bot walking west to its point, not fighting", d)
	}
	if f.travel(&bots.Team{}, b, 22, f.it); f.standsIn(b) != east {
		t.Fatalf("an enemy in their own base: the bot stopped at (%.1f, %.1f); want it gone to the east point", f.self.State.X, f.self.State.Z)
	}
}

// pvp.md section 8, as built: a battle's side shares no focus, as its bots are at three points; each fights the nearest it can reach. Heals reach one of the side within 12 m alone.
func TestABattleSideSharesNoFocusAndHealsNearOnly(t *testing.T) {
	f := newBattleFight(t, "warrior", 50, 45)
	f.self.Buffs = []string{"war_cry"}
	b := f.battle(15, 0, 0, 0)
	full, low := f.other(2, 0, 1.2, 100), f.other(2, 0, -1.3, 30)
	if got := lockOf(f.decideBattle(&bots.Team{}, b, 20, full, low)); got != full.ID {
		t.Fatalf("in a battle the bot fought %v; want the nearest, %v, not the lowest", got, full.ID)
	}
	heal := func(d bots.Decision) bool { return d.Skill != nil && d.Skill.Skill == "heal" }
	f = newBattleFight(t, "magician", 50, 45)
	f.self.HP, f.self.MaxHP = 100, 100
	f.it.Body.X, f.it.Body.Z = 50, 45+5.5 // in a magician's band: it holds still
	if d := f.decideBattle(&bots.Team{}, b, 20, f.it, f.other(1, 2, 0, 50)); !heal(d) {
		t.Fatalf("a teammate at 50%% 2 m off: %+v; want a heal", d)
	}
	f = newBattleFight(t, "magician", 50, 45)
	f.self.HP, f.self.MaxHP = 100, 100
	f.it.Body.X, f.it.Body.Z = 50, 45+5.5
	if d := f.decideBattle(&bots.Team{}, b, 20, f.it, f.other(1, 0, -13, 40)); d.Skill != nil && d.Skill.Skill == "heal" || south(d.Keys) {
		t.Fatalf("a teammate at 40%% 13 m south: %+v; want no heal and no walk to them", d)
	}
}

// The spots bots stand on in a point spread within half its radius, each one a body fits.
func TestBattleBotsSpreadInAPoint(t *testing.T) {
	var at [][2]float64
	for k := range 4 {
		f := newBattleFight(t, "warrior", 26, 45+float64(k))
		f.setup.Seed = [2]uint64{uint64(k), 7}
		f.brain = bots.New(f.setup)
		b := f.battle(15, 0, 0, 0)
		f.travel(&bots.Team{}, b, 20, f.it)
		if f.standsIn(b) != west || !f.setup.Grid.Fits(nav.Point{X: f.self.State.X, Z: f.self.State.Z}, f.setup.Radius) {
			t.Fatalf("bot %d stopped at (%.1f, %.1f); want it in the west point, where it fits", k, f.self.State.X, f.self.State.Z)
		}
		at = append(at, [2]float64{f.self.State.X, f.self.State.Z})
	}
	for j := range at {
		for k := j + 1; k < len(at); k++ {
			if math.Hypot(at[j][0]-at[k][0], at[j][1]-at[k][1]) < 0.5 {
				t.Fatalf("bots %d and %d stopped together at %v and %v; want them spread", j, k, at[j], at[k])
			}
		}
	}
}

// A spot rolled where a body can't stand, on a point whose circle reaches over a boulder, gives way to the point's middle.
func TestABotStandsInAPointsMiddleWhenItsSpotWontTakeIt(t *testing.T) {
	middles := 0
	for k := range 20 {
		f := newBattleFight(t, "warrior", 45, 47)
		f.setup.Seed = [2]uint64{uint64(k), 11}
		f.brain = bots.New(f.setup)
		b := f.battle(15, 0, 0, 0)
		b.Points = []bots.Point{{At: gamedata.Point{X: 45, Z: 41}, Radius: 6}} // the boulder at (45, 37), 2.52 m round, under its south side
		f.travel(&bots.Team{}, b, 20, f.it)
		at := nav.Point{X: f.self.State.X, Z: f.self.State.Z}
		if !f.setup.Grid.Fits(at, f.setup.Radius) {
			t.Fatalf("seed %d: the bot stopped at (%.1f, %.1f), where no body fits; want its spot moved to the middle", k, at.X, at.Z)
		}
		if nav.Distance(at, nav.Point{X: 45, Z: 41}) <= 0.5 {
			middles++
		}
	}
	if middles == 0 {
		t.Fatal("no seed rolled a spot in the boulder, so nothing checked the fallback")
	}
}
