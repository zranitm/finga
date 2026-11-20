package mobs_test

import (
	"math"
	"slices"
	"testing"

	"finga/internal/application/world/mobs"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// windup steps a from tick from until a mob's windup starts, failing after 3 s, and returns it and the next tick.
func windup(t *testing.T, a *mobs.Arena, players []mobs.Body, from uint64) (model.MobAttacked, uint64) {
	t.Helper()
	for n := from; n < from+60; n++ {
		a.Step(n, players, nil)
		if s := a.Started(); len(s) > 0 {
			return s[0], n + 1
		}
	}
	t.Fatal("no windup in 3 s")
	return model.MobAttacked{}, 0
}

// landed steps a from tick from through the attack begun before it, lasting ticks, and returns the hits of each landing, in order.
func landed(a *mobs.Arena, players []mobs.Body, from, ticks uint64) []mobs.Hit {
	var hits []mobs.Hit
	for n := from; n < from+ticks; n++ {
		a.Step(n, players, nil)
		for _, l := range a.Landings() {
			hits = append(hits, l.Hits...)
		}
	}
	return hits
}

// A bite fixes its arc at the windup, turned to its target: it lands on them if they stay, knocking them away, and on nobody if they step out.
func TestABiteLandsOnlyOnAPlayerStillInItsArc(t *testing.T) {
	for _, stays := range []bool{true, false} {
		a := boarAt(t, open(), 20, 20)
		v := a.Views()[0]
		p := player(20, v.Z-1.3) // 5 cm short of touching, south
		at, next := windup(t, a, []mobs.Body{p}, 1)
		if at.ToX != v.X || at.ToZ != v.Z || math.Abs(math.Abs(float64(at.Yaw))-math.Pi) > 1e-3 {
			t.Fatalf("windup %+v; want it where the boar stands, turned south", at)
		}
		if !stays {
			p.Z -= 3
		}
		hits := landed(a, []mobs.Body{p}, next, 12) // the windup's 600 ms
		if !stays {
			if len(hits) != 0 {
				t.Fatalf("hits %+v; want none on a player who stepped out", hits)
			}
			continue
		}
		if len(hits) == 0 || hits[0].Player != p.ID || hits[0].DZ > -0.99 {
			t.Fatalf("hits %+v; want the player knocked south", hits)
		}
	}
}

// A hop's landing is fixed at the windup, touching its target: it lands there on them, and on nobody who stepped aside.
func TestAHopLandsTouchingWhereItsTargetStood(t *testing.T) {
	for _, stays := range []bool{true, false} {
		a := alone(t, "green_slime")
		v := a.Views()[0]
		p := player(20, v.Z-3)
		at, next := windup(t, a, []mobs.Body{p}, 1)
		if math.Abs(at.ToX-20) > 1e-6 || math.Abs(at.ToZ-(p.Z+0.875)) > 1e-6 { // its yaw is a float32
			t.Fatalf("windup %+v; want its landing touching the player at z %v", at, p.Z+0.875)
		}
		if !stays {
			p.X += 3
		}
		hits := landed(a, []mobs.Body{p}, next, 20) // the windup's 600 ms and the travel's 400
		if v := a.Views()[0]; math.Abs(v.X-at.ToX) > 1e-9 || math.Abs(v.Z-at.ToZ) > 1e-9 {
			t.Fatalf("the slime landed at (%v, %v); want (%v, %v)", v.X, v.Z, at.ToX, at.ToZ)
		}
		if got := len(hits) > 0 && hits[0].Player == p.ID; got != stays {
			t.Fatalf("hits %+v; want a hit %v", hits, stays)
		}
	}
}

// A charge runs its lane and ends on the first player it meets, stopping at their side; a wall or a mob cuts it short.
func TestAChargeEndsOnTheFirstPlayerItMeetsOrAWallOrAMob(t *testing.T) {
	t.Run("two players in its lane", func(t *testing.T) {
		a := newArena(t, meadow(t, open(), spawn("orange_mushroom", 1, 20, 20, 0.01)), worldtest.FixedRoll{V: 0.5})
		v := a.Views()[0]
		near, far := player(20, v.Z-2.5), player(20, v.Z-4)
		_, next := windup(t, a, []mobs.Body{near, far}, 1)
		hits := landed(a, []mobs.Body{near, far}, next, 24) // the windup's 800 ms and the travel's 400
		if len(hits) == 0 || hits[0].Player != near.ID {
			t.Fatalf("hits %+v; want the near player first", hits)
		}
		if v := a.Views()[0]; v.Z < near.Z+0.5625+playerRadius-1e-9 {
			t.Fatalf("the mushroom ran on to z %v, into the near player at %v", v.Z, near.Z)
		}
	})
	t.Run("a wall in its lane", func(t *testing.T) {
		wall := sim.Box{X: 20, Z: 18.5, HalfW: 3, HalfD: 0.25, UX: 1, Top: 3}
		a := newArena(t, meadow(t, open(wall), spawn("orange_mushroom", 1, 20, 20, 0.01)), worldtest.FixedRoll{V: 0.5})
		p := player(20, 17)
		at, next := windup(t, a, []mobs.Body{p}, 1)
		if math.Abs(at.ToZ-(18.75+0.5625)) > 1e-9 {
			t.Fatalf("windup %+v; want its end at the wall's face, z %v", at, 18.75+0.5625)
		}
		if hits := landed(a, []mobs.Body{p}, next, 24); len(hits) != 0 {
			t.Fatalf("hits %+v; want none through the wall", hits)
		}
	})
	// charge runs the mushroom's charge at a player at (20, 16) beside a slime that starts at (slimeX, 16.875), and returns its windup, its views after, and whether it hit.
	charge := func(t *testing.T, data *gamedata.Data, slimeX float64) (model.MobAttacked, []model.MobView, bool) {
		m := meadow(t, open(), spawn("orange_mushroom", 1, 20, 20, 0.01), spawn("green_slime", 1, slimeX, 16.875, 0.01))
		a := mobs.NewArena(mobs.Deps{Map: m, Data: data, Roll: worldtest.FixedRoll{V: 0.5}}, mobs.Tuning{TickInterval: tick})
		p := []mobs.Body{player(20, 16)}
		var at model.MobAttacked
		hit := false
		for n, end := uint64(1), uint64(0); end == 0 || n < end; n++ {
			if n > 60 {
				t.Fatal("no charge in 3 s")
			}
			a.Step(n, p, nil)
			for _, s := range a.Started() {
				if s.Mob == 1 && end == 0 {
					at, end = s, n+25 // the windup's 800 ms and the travel's 400
				}
			}
			for _, l := range a.Landings() {
				hit = hit || l.Mob == 1
			}
		}
		return at, a.Views(), hit
	}
	for _, cell := range []string{"32", "1"} { // in 1 m cells, the slime's cell is far from the mushroom's
		t.Run("a mob waiting beside the player in its lane, cells of "+cell+" m", func(t *testing.T) {
			at, v, hit := charge(t, withCell(t, cell), 20) // the slime touches the player, between them and the mushroom
			touching := 16.875 + 0.5 + 0.5625
			if math.Abs(at.ToZ-touching) > 1e-9 {
				t.Fatalf("windup %+v; want its end, which the telegraph's lane runs to, touching the slime at z %v", at, touching)
			}
			if hit || math.Abs(v[0].Z-touching) > 1e-6 {
				t.Fatalf("the mushroom ended at z %v, hitting %v; want it stopped touching the slime, short of the player", v[0].Z, hit)
			}
		})
	}
	t.Run("a mob that steps into its lane after the windup", func(t *testing.T) {
		at, v, hit := charge(t, worldtest.LoadData(t), 21.2) // off the lane at the windup, the slime hops to the player's side, into it
		if at.ToZ > 16.5 {
			t.Fatalf("windup %+v; want it run to the player, the slime off its lane then", at)
		}
		if gap := distance(v[0], v[1].X, v[1].Z) - (0.5 + 0.5625); hit || math.Abs(gap) > 1e-6 {
			t.Fatalf("the mushroom ended %.4f m from touching the slime, hitting %v; want it stopped touching it, short of the player", gap, hit)
		}
	})
}

// A charge from beside the player lands, though the chargers beside it have their edges in its lane: four mushrooms ringing a still player land their charges, and one held behind the ring lands none through it.
func TestChargersRingingAPlayerLandTheirCharges(t *testing.T) {
	a := newArena(t, meadow(t, open(), spawn("orange_mushroom", 4, 20, 23, 2)), worldtest.FixedRoll{V: 0.5})
	p := player(20, 20)
	beside, landed := 0, 0
	behind := map[uint64]bool{}          // whether each mob's latest charge began behind the ring
	for n := uint64(1); n <= 1200; n++ { // a minute
		a.Step(n, []mobs.Body{p}, nil)
		for _, s := range a.Started() {
			v := slices.IndexFunc(a.Views(), func(v model.MobView) bool { return v.ID == s.Mob })
			gap := distance(a.Views()[v], p.X, p.Z) - 0.5625 - playerRadius
			if gap < 0.1 {
				beside++
			}
			behind[s.Mob] = gap > 1
		}
		for _, l := range a.Landings() {
			if behind[l.Mob] {
				t.Errorf("tick %d: mob %d's charge from behind the ring landed through it", n, l.Mob)
			}
			landed += len(l.Hits)
		}
	}
	if beside < 40 || float64(landed) < 0.85*float64(beside) {
		t.Fatalf("%d charges landed of %d begun beside the player; want nearly all", landed, beside)
	}
}

// A neighbour walking past a charger's side, already overlapping it, doesn't cut the charge off from a player just ahead: it goes no more than crowdedBy deeper into the neighbour before it meets them.
func TestAChargePastANeighbourAtItsSideLandsOnAPlayerJustAhead(t *testing.T) {
	a := newArena(t, meadow(t, open(), spawn("green_slime", 1, 20.375, 20, 0.001), spawn("orange_mushroom", 1, 20, 20, 0.001)), worldtest.FixedRoll{V: 0.5})
	p := []mobs.Body{player(20, a.Views()[1].Z-0.5625-playerRadius-0.05)}
	var at model.MobAttacked
	n := uint64(1)
	for ; at.Mob == 0; n++ {
		if n > 60 {
			t.Fatal("no charge in 3 s")
		}
		a.Step(n, p, nil)
		for _, s := range a.Started() {
			if s.Mob == 2 {
				at = s
			}
		}
		if at.Mob == 0 {
			continue
		}
		slime, mushroom := a.Views()[0], a.Views()[1]
		dx, dz := slime.X-mushroom.X, slime.Z-mushroom.Z
		hx, hz := model.HeadingOf(at.Yaw)
		off := math.Acos((dx*hx+dz*hz)/math.Hypot(dx, dz)) * 180 / math.Pi
		overlap := 0.5 + 0.5625 - math.Hypot(dx, dz)
		gap := distance(mushroom, p[0].X, p[0].Z) - 0.5625 - playerRadius
		if overlap < 0.15 || overlap > 0.25 || off < 55 || off > 65 || gap > 0.15 {
			t.Fatalf("at the windup the slime overlaps the mushroom by %.3f m, %.1f° off its heading, the player %.3f m ahead; want the case: about 0.2 m, 60°, a few cm", overlap, off, gap)
		}
	}
	if hits := landed(a, p, n, 26); len(hits) == 0 { // the windup's 800 ms and the travel's 400
		t.Fatalf("windup %+v: the charge landed on no one; want it on the player just ahead", at)
	}
}

// A spit flies along its windup's aim: it lands on its target there, unless they step behind a wall while it winds up and the wall takes the ball.
func TestASpitFliesToItsTargetUnlessAWallStopsIt(t *testing.T) {
	for _, walled := range []bool{false, true} {
		g := open()
		if walled {
			g = open(sim.Box{X: 20, Z: 17, HalfW: 3, HalfD: 0.25, UX: 1, Top: 3})
		}
		a := newArena(t, meadow(t, g, spawn("cave_bat", 1, 20, 20, 0.01)), worldtest.FixedRoll{V: 0.5})
		v := a.Views()[0]
		p := player(20, v.Z-2) // short of the wall, in the bat's clear line
		back := p
		back.Z = v.Z - 6 // then behind it
		if _, next := windup(t, a, []mobs.Body{p}, 1); distance(a.Views()[0], v.X, v.Z) > 1e-9 {
			t.Fatal("the bat moved to spit; want it holding where its spit reaches")
		} else if hits := landed(a, []mobs.Body{back}, next, 24); (len(hits) > 0) == walled { // the windup's 600 ms and 6 m at 9.375 m/s
			t.Fatalf("walled %v: hits %+v", walled, hits)
		}
	}
}

// Deep water stops bodies, not shots (large-regions.md decision 9): a bat spits across a strip of it, standing where it is, and the ball lands on the far side.
func TestASpitFliesOverDeepWater(t *testing.T) {
	g := sim.Geometry{SizeX: 40, SizeZ: 40, Terrain: sim.NewTerrain(1, 41, 41, make([]float64, 41*41), 0.7), Boxes: []sim.Box{}, Cylinders: []sim.Cylinder{}}
	g.Terrain.DeepWater = make([]bool, 41*41)
	for x := range 41 {
		g.Terrain.DeepWater[17*41+x] = true // cells from z 16 to 18
	}
	a := newArena(t, meadow(t, g, spawn("cave_bat", 1, 20, 20, 0.01)), worldtest.FixedRoll{V: 0.5})
	v := a.Views()[0]
	p := player(20, v.Z-6)
	if _, next := windup(t, a, []mobs.Body{p}, 1); distance(a.Views()[0], v.X, v.Z) > 1e-9 {
		t.Fatal("the bat moved to spit; want it spitting across the water from where it stands")
	} else if hits := landed(a, []mobs.Body{p}, next, 24); len(hits) == 0 {
		t.Fatal("the spit landed on no one; want it over the water and on the player")
	}
}

// A spitter doesn't spit at a target a wall hides from its ball: it walks round the wall, and each spit it starts lands on them. The boss spits the same.
func TestASpitterWalksRoundAWallToSpit(t *testing.T) {
	for _, id := range []string{"cave_bat", "vampire_bat"} {
		t.Run(id, func(t *testing.T) {
			def, _ := worldtest.LoadData(t).Mob(id)
			a := newArena(t, meadow(t, open(sim.Box{X: 20, Z: 17, HalfW: 3, HalfD: 0.25, UX: 1, Top: 3}), spawn(id, 1, 20, 20, 0.01)), worldtest.FixedRoll{V: 0.5})
			v := a.Views()[0]
			p := player(20, v.Z-6) // in range, straight behind the wall
			got := stepThrough(a, 1, 400, p)
			var spits []model.MobAttacked
			for _, s := range got.started {
				if def.Attacks[s.Attack].Kind == gamedata.Shot && s.Tick < 360 { // its ball has landed by tick 400
					spits = append(spits, s)
				}
			}
			if len(spits) == 0 {
				t.Fatalf("windups %+v; want a spit in 18 s", got.started)
			}
			if first := spits[0]; math.Hypot(first.ToX-v.X, first.ToZ-v.Z) < 3 {
				t.Fatalf("first spit from (%.2f, %.2f); want it from round the wall, not where it stood behind it", first.ToX, first.ToZ)
			}
			hits := 0
			for _, l := range got.landed {
				if def.Attacks[l.Attack].Kind == gamedata.Shot && len(l.Hits) == 1 && l.Hits[0].Player == p.ID {
					hits++
				}
			}
			if hits < len(spits) {
				t.Fatalf("%d of %d spits landed on the player; want each, none thrown into the wall", hits, len(spits))
			}
		})
	}
}

// happened is what a run of ticks started and landed.
type happened struct {
	started   []model.MobAttacked
	landed    []mobs.Landing
	landTicks []uint64 // the tick of each landing
}

// startTicks are the ticks of the windups, in order.
func (h happened) startTicks() []uint64 {
	var ticks []uint64
	for _, s := range h.started {
		ticks = append(ticks, s.Tick)
	}
	return ticks
}

// stepThrough steps a from tick from to tick to, both included, among players.
func stepThrough(a *mobs.Arena, from, to uint64, players ...mobs.Body) happened {
	var h happened
	for n := from; n <= to; n++ {
		a.Step(n, players, nil)
		h.started = append(h.started, a.Started()...)
		for _, l := range a.Landings() {
			h.landed, h.landTicks = append(h.landed, l), append(h.landTicks, n)
		}
	}
	return h
}

// slay is a strike at (x, z) that kills whatever mob stands there.
func slay(x, z float64) mobs.Strike {
	return mobs.Strike{By: kernel.NewCharacterID(), Shape: gamedata.Shape{Kind: gamedata.AreaShape, Radius: 0.01}, Most: 1, Power: strong, Aim: model.Aim{X: x, Z: z}}
}

// heading is the unit vector a mob's yaw faces in x and z.
func heading(v model.MobView) (float64, float64) {
	return math.Sin(float64(v.Yaw)), math.Cos(float64(v.Yaw))
}

// A boar bites a player in reach after its wait and its 600 ms windup, and again the tick its 2 s cooldown from that windup runs out: a cooldown isn't leaving its reach, so no second wait.
func TestABiteLandsAgainAfterItsCooldown(t *testing.T) {
	a := alone(t, "wild_boar")
	p := player(20, a.Views()[0].Z-1.3) // on its contact ring, south
	got := stepThrough(a, 1, 60+wait, p)
	if want := []uint64{1 + wait, 41 + wait}; !slices.Equal(got.startTicks(), want) {
		t.Fatalf("windups at %v; want %v", got.startTicks(), want)
	}
	if want := []uint64{13 + wait, 53 + wait}; !slices.Equal(got.landTicks, want) {
		t.Fatalf("landed at %v; want 600 ms after each windup", got.landTicks)
	}
	for _, l := range got.landed {
		if len(l.Hits) != 1 || l.Hits[0].Player != p.ID {
			t.Fatalf("landing %+v; want the bite on the player", l)
		}
	}
}

// A boss takes its two attacks in turn, the hop and the slam, or whichever is ready when only one is.
func TestABossAlternatesItsAttacks(t *testing.T) {
	a := alone(t, "king_slime")
	got := stepThrough(a, 1, 230+wait, player(20, a.Views()[0].Z-1.925)) // on its contact ring, south
	var order [][2]uint64
	for _, s := range got.started {
		order = append(order, [2]uint64{s.Tick, uint64(s.Attack)})
	}
	// The hop is busy 1.3 s and ready every 4.75 s, the slam busy 1.6 s and ready every 9.25 s, so the slam ready at 212 waits for the hop.
	if want := [][2]uint64{{1 + wait, 0}, {27 + wait, 1}, {96 + wait, 0}, {191 + wait, 0}, {217 + wait, 1}}; !slices.Equal(order, want) {
		t.Fatalf("attacks by tick %v; want %v", order, want)
	}
}

// A mob killed this tick lands nothing, and its spit in flight goes with it.
func TestAKilledMobLandsNothing(t *testing.T) {
	a := alone(t, "wild_boar")
	v := a.Views()[0]
	p := player(20, v.Z-1.3)
	stepThrough(a, 1, 12+wait, p)
	if a.Step(13+wait, []mobs.Body{p}, nil); !bitten(a, p.ID) {
		t.Fatalf("landings %+v; want the bite", a.Landings())
	}
	if blow, _ := one(t, a.Strike(slay(v.X, v.Z))); blow.Killed == nil {
		t.Fatalf("blow %+v; want the boar killed", blow)
	}
	if len(a.Landings()) != 0 {
		t.Fatalf("landings %+v after the boar died; want none", a.Landings())
	}

	a = alone(t, "cave_bat")
	v = a.Views()[0]
	p = player(20, v.Z-6)
	if got := stepThrough(a, 1, 13+wait, p); len(got.started) != 1 || len(got.landed) != 0 { // its spit thrown at tick 13+wait, 6 m to fly
		t.Fatalf("windups %+v, landings %+v; want the spit thrown and in flight", got.started, got.landed)
	}
	if blow, _ := one(t, a.Strike(slay(v.X, v.Z))); blow.Killed == nil {
		t.Fatalf("blow %+v; want the bat killed", blow)
	}
	if got := stepThrough(a, 14+wait, 40+wait, p); len(got.landed) != 0 {
		t.Fatalf("landings %+v; want the dead bat's spit gone", got.landed)
	}
}

// A mob's attack meets the defense of its damage type: a slime's physical, a bat's magic.
func TestAnAttackMeetsTheDefenseOfTheMobsDamageType(t *testing.T) {
	hit := func(mob string, d model.Defender) int {
		t.Helper()
		a := alone(t, mob)
		p := player(20, a.Views()[0].Z-1.5)
		got := stepThrough(a, 1, 40, p)
		if len(got.landed) == 0 {
			t.Fatalf("%s landed nothing", mob)
		}
		dealt := a.Roll(got.landed[0], p.ID, d)
		if dealt.Miss || dealt.Target.Player != p.ID || !dealt.Source.IsMob() {
			t.Fatalf("%s: %+v", mob, dealt)
		}
		return dealt.Amount
	}
	walls := model.Defender{PhysicalDefense: 100, MagicDefense: 100}
	// Bat attack 22: 22·100/200 = 11 against either 100; a slime's 6: 6·100/200 = 3.
	for _, c := range []struct {
		mob        string
		d          model.Defender
		want       int
		meaningFor string
	}{
		{"cave_bat", model.Defender{PhysicalDefense: 100}, 22, "a bat ignores physical defense"},
		{"cave_bat", walls, 11, "a bat meets magic defense"},
		{"green_slime", model.Defender{MagicDefense: 100}, 6, "a slime ignores magic defense"},
		{"green_slime", walls, 3, "a slime meets physical defense"},
	} {
		if got := hit(c.mob, c.d); got != c.want {
			t.Errorf("%s: %d damage, want %d", c.meaningFor, got, c.want)
		}
	}
}

// A melee mob bites from where its bite first reaches, then walks on to its spot at its target's side while the bite cools.
func TestAMobClosesInWhileItsAttackCools(t *testing.T) {
	a := alone(t, "wild_boar")
	v := a.Views()[0]
	p := player(20, v.Z-1.75) // in the bite's reach, 0.45 m off its contact ring
	got := stepThrough(a, 1, 40, p)
	if len(got.started) != 1 || got.started[0].Tick != 1+wait || got.started[0].ToZ != v.Z {
		t.Fatalf("windups %+v; want the bite from where the boar stood at tick %d", got.started, 1+wait)
	}
	if d := distance(a.Views()[0], p.X, p.Z); math.Abs(d-1.3) > 1e-9 {
		t.Fatalf("the boar %.3f m off the player; want it walked on to its contact ring, 1.3 m", d)
	}
}

// A mob that shoots holds once its target is in its shot's range, walking in only until then, and stays there while its spit cools.
func TestAShooterHoldsAtRange(t *testing.T) {
	const inRange = 0.375 + 7.5 + 0.375/2 // the most a bat's centre and a player's may be apart for its spit
	for _, tt := range []struct {
		name string
		off  float64
	}{{"in range from the start", 6}, {"1 m past its range", inRange + 1}} {
		t.Run(tt.name, func(t *testing.T) {
			a := alone(t, "cave_bat")
			bat, _ := worldtest.LoadData(t).Mob("cave_bat")
			step := bat.Speed * tick.Seconds() // its pace is 1 at a roll of 0.5
			p := player(20, a.Views()[0].Z-tt.off)
			got := stepThrough(a, 1, 40, p)
			if len(got.started) == 0 {
				t.Fatal("no spit in 2 s")
			}
			v := a.Views()[0]
			if d := distance(v, p.X, p.Z); d > inRange+1e-9 || d < min(tt.off, inRange-step)-1e-9 {
				t.Fatalf("the bat %.3f m off the player; want it where it stood if in range, else within a step of where its spit first reaches, %.3f m", d, inRange)
			}
			if got := stepThrough(a, 41, 100, p); len(got.started) == 0 || distance(a.Views()[0], v.X, v.Z) != 0 {
				t.Fatalf("windups %+v, the bat moved to (%.3f, %.3f); want it spitting again from where it held", got.started, a.Views()[0].X, a.Views()[0].Z)
			}
		})
	}
}

// A mob keeps its target while it sees them; out of its sight it keeps after them 1 s, turning as they move, and then lets them go.
func TestAMobKeepsItsTargetASecondOutOfSight(t *testing.T) {
	a := alone(t, "wild_boar")
	home := a.Views()[0]
	p := player(20, 14)
	stepThrough(a, 1, 10, p) // last seen at tick 10
	unseen := func(dx float64) mobs.Body {
		v := a.Views()[0]
		q := up(p, 3.5)
		q.X, q.Z = v.X+dx, v.Z
		return q
	}
	stepThrough(a, 11, 20, unseen(6))
	if ux, _ := heading(a.Views()[0]); ux < 0.7 {
		t.Fatalf("boar %+v; want it turned east after the unseen player", a.Views()[0])
	}
	stepThrough(a, 21, 29, unseen(-6))
	if ux, _ := heading(a.Views()[0]); ux > -0.7 {
		t.Fatalf("boar %+v; want it turned west after them at tick 29, 19 ticks unseen", a.Views()[0])
	}
	stepThrough(a, 30, 30, unseen(-6))
	w := a.Views()[0]
	ux, uz := heading(w)
	if back := math.Hypot(home.X-w.X, home.Z-w.Z); float64(ux*(home.X-w.X))+float64(uz*(home.Z-w.Z)) < 0.9*back {
		t.Fatalf("boar %+v; want it turned to its circle, the player let go at tick 30", w)
	}
}

// A provoked mob follows its attacker 5 s past its sight, then lets them go and walks back to its circle with no second kept for a player it never saw.
func TestAProvokedMobFollowsItsAttackerThenWalksBack(t *testing.T) {
	a := alone(t, "green_slime")
	home := a.Views()[0]
	p := player(20, 5) // 15 m off: out of its sight even after following them 5 m
	a.Step(1, []mobs.Body{p}, nil)
	s := slay(home.X, home.Z)
	s.By, s.Power = p.ID, model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 1, Accuracy: 0} // a MISS provokes too
	if blow, _ := one(t, a.Strike(s)); !blow.Damage.Miss {
		t.Fatalf("blow %+v; want a MISS, which neither flinches nor pushes", blow)
	}
	stepThrough(a, 2, 101, p) // the provoke's 100 turns at 0.05 m each
	v := a.Views()[0]
	if d := distance(v, home.X, home.Z); d < 4.5 || d > 5+1e-9 || v.Z > home.Z-4.5 {
		t.Fatalf("slime at (%.3f, %.3f) at tick 101; want it 4.5 to 5 m south, still following", v.X, v.Z)
	}
	stepThrough(a, 102, 102, p)
	if w := a.Views()[0]; w.Z <= v.Z {
		t.Fatalf("slime at (%.3f, %.3f) at tick 102; want it walking back north", w.X, w.Z)
	}
	stepThrough(a, 103, 230, p)
	if w := a.Views()[0]; distance(w, home.X, home.Z) > 0.01 {
		t.Fatalf("slime at (%.3f, %.3f) at tick 230; want it back at its spot", w.X, w.Z)
	}
}

// A mob attacks a player other than its target only when it can't get closer to its target: in its way, or waiting with its target out of its reach.
func TestAMobAttacksWhoItCanReachOnlyWhenStuck(t *testing.T) {
	t.Run("free to walk on", func(t *testing.T) {
		a := alone(t, "wild_boar")
		far := player(20, 27)
		a.Step(1, []mobs.Body{far}, nil) // its target: the only player it sees, kept while it sees them
		v := a.Views()[0]
		beside := player(v.X-1.3, v.Z) // in its bite's reach, out of its way
		if got := stepThrough(a, 2, 30, far, beside); len(got.started) != 0 || a.Views()[0].Z < v.Z+2 {
			t.Fatalf("windups %+v, boar %+v; want it walking on to its target, no bite at the player beside it", got.started, a.Views()[0])
		}
	})
	for _, cell := range []string{"32", "1"} { // in 1 m cells, the player beside the boar is in another cell
		boar := func(t *testing.T, g sim.Geometry) *mobs.Arena {
			return mobs.NewArena(mobs.Deps{Map: meadow(t, g, spawn("wild_boar", 1, 20, 20, 0.01)), Data: withCell(t, cell), Roll: worldtest.FixedRoll{V: 0.5}}, mobs.Tuning{TickInterval: tick})
		}
		stuck(t, cell, boar)
	}
}

// stuck runs a boar that boar makes, at (20, 20) on ground g, stuck against a player in its way, and waiting under its target, in cells of cell metres.
func stuck(t *testing.T, cell string, boar func(t *testing.T, g sim.Geometry) *mobs.Arena) {
	t.Run("a player in its way, cells of "+cell+" m", func(t *testing.T) {
		a := boar(t, open())
		far := player(20, 27)
		a.Step(1, []mobs.Body{far}, nil)
		v := a.Views()[0]
		block := player(v.X, v.Z+1.25) // touching its north side, between it and its target
		got := stepThrough(a, 2, 40, far, block)
		if len(got.started) != 1 || len(got.landed) != 1 || got.landed[0].Hits[0].Player != block.ID {
			t.Fatalf("windups %+v, landings %+v; want a bite on the player in its way once it stalled against them and waited", got.started, got.landed)
		}
		if w := got.started[0]; w.Yaw != model.YawOf(block.X-w.ToX, block.Z-w.ToZ) {
			t.Fatalf("windup %+v; want it turned to the player in its way", w)
		}
	})
	t.Run("waiting under its target, cells of "+cell+" m", func(t *testing.T) {
		g := open(sim.Box{X: 20, Z: 26, HalfW: 1.5, HalfD: 1.5, UX: 1, Top: 2})
		a := boar(t, g)
		far := up(player(20, 26), 2) // on a block 2 m tall: seen, out of reach, no spot at their side the boar fits
		stepThrough(a, 1, 60, far)
		v := a.Views()[0]
		if got := stepThrough(a, 61, 70, far); len(got.started) != 0 || distance(a.Views()[0], v.X, v.Z) != 0 {
			t.Fatalf("windups %+v, boar %+v; want it waiting still at its spot", got.started, a.Views()[0])
		}
		near := player(v.X+1.6, v.Z) // come into its bite's reach
		if got := stepThrough(a, 71, 71+wait, far, near); len(got.started) != 1 || got.started[0].Tick != 71+wait || math.Abs(float64(got.started[0].Yaw)-math.Pi/2) > 1e-3 {
			t.Fatalf("windups %+v; want a bite east at the player come beside it, after its wait", got.started)
		}
	})
}

// Two slimes on one spot beside a player don't attack from it: each walks to a place of its own first.
func TestMobsOnOneSpotDontAttackTillTheyHaveParted(t *testing.T) {
	a := slimes(t, 0.5) // both on (20, 20 - √2)
	p := player(20, 20-math.Sqrt2+0.9)
	slime, _ := worldtest.LoadData(t).Mob("green_slime")
	attacked := map[uint64]bool{}
	for n := uint64(1); n <= 200; n++ {
		a.Step(n, []mobs.Body{p}, nil)
		v := a.Views()
		apart := distance(v[0], v[1].X, v[1].Z)
		for _, s := range a.Started() {
			if overlap := 2*slime.Radius - apart; overlap > 0.25 {
				t.Fatalf("tick %d: mob %d began an attack %.3f m into the other; want them parted first", n, s.Mob, overlap)
			}
			attacked[s.Mob] = true
		}
	}
	if len(attacked) != 2 {
		t.Fatalf("attacks by %v; want both, once parted", attacked)
	}
}
