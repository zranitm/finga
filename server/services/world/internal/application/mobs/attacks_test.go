package mobs_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/world/internal/application/mobs"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// alone is one mob of type id on the slimes' platform, at x 200 facing right, its ground the area's 100 to 300.
func alone(t *testing.T, id string, edits ...func(*gamedata.Map)) *mobs.Arena {
	t.Helper()
	m := slimes(time.Hour)
	m.Spawns[0].Mob, m.Spawns[0].Max = id, 1
	for _, edit := range edits {
		edit(m)
	}
	return mobs.NewArena(mobs.Deps{Map: m, Data: worldtest.LoadData(t), Roll: worldtest.FixedRoll{V: 0.5}}, mobs.Tuning{TickInterval: tick})
}

// standing is a player with their feet's centre at x on the floor.
func standing(x float64) mobs.Body {
	return mobs.Body{ID: kernel.NewCharacterID(), Box: model.Rect{X: x - 12, Y: floor - 56, W: 24, H: 56}}
}

// at is p moved to stand at x.
func at(p mobs.Body, x float64) mobs.Body {
	p.Box.X = x - 12
	return p
}

// played is what a run of ticks started and landed, by tick.
type played struct {
	started []model.MobAttacked
	landed  map[uint64][]mobs.Landing
}

// play steps the arena from tick from to tick to among players, as where(n) places them.
func play(a *mobs.Arena, from, to uint64, where func(n uint64) []mobs.Body) played {
	out := played{landed: map[uint64][]mobs.Landing{}}
	for n := from; n <= to; n++ {
		a.Step(n, where(n))
		out.started = append(out.started, a.Started()...)
		if l := a.Landings(); len(l) > 0 {
			out.landed[n] = slices.Clone(l)
		}
	}
	return out
}

func still(players ...mobs.Body) func(uint64) []mobs.Body {
	return func(uint64) []mobs.Body { return players }
}

// landedTicks are the ticks something landed, in order.
func (p played) landedTicks() []uint64 {
	return slices.Sorted(func(yield func(uint64) bool) {
		for n := range p.landed {
			if !yield(n) {
				return
			}
		}
	})
}

// C12: a boar bites a player in its reach after its 600 ms windup, and again once its 2 s cooldown from the start runs out; it holds, facing them.
func TestABiteLandsAfterItsWindupAndAgainAfterItsCooldown(t *testing.T) {
	a := alone(t, "wild_boar") // 56 wide: its box 172-228
	boar := a.Views()[0].ID
	p := standing(240) // 228-252, in the bite's 24 px
	got := play(a, 1, 60, still(p))
	if want := []model.MobAttacked{{Tick: 1, Mob: boar, Attack: 0, Facing: 1}, {Tick: 41, Mob: boar, Attack: 0, Facing: 1}}; !slices.Equal(got.started, want) {
		t.Fatalf("windups %+v; want %+v", got.started, want)
	}
	if ticks := got.landedTicks(); !slices.Equal(ticks, []uint64{13, 53}) {
		t.Fatalf("landed at ticks %v; want 13 and 53, 600 ms after each windup", ticks)
	}
	if l := got.landed[13]; len(l) != 1 || l[0].Mob != boar || l[0].Attack != 0 || !slices.Equal(l[0].Hits, []mobs.Hit{{Player: p.ID, Dir: 1}}) {
		t.Fatalf("landing %+v; want the bite on the player, knocking them right", l)
	}
	if v := a.Views()[0]; v.X != spawnX || v.Facing != 1 {
		t.Fatalf("boar %+v; want it holding where it stood, facing the player", v)
	}
}

// C12: a player who steps out of the bite's reach during its windup isn't hit.
func TestSteppingOutOfAWindupDodgesIt(t *testing.T) {
	a := alone(t, "wild_boar")
	p := standing(240)
	got := play(a, 1, 20, func(n uint64) []mobs.Body {
		if n <= 6 {
			return []mobs.Body{p}
		}
		return []mobs.Body{at(p, 300)}
	})
	if len(got.started) != 1 || len(got.landed) != 0 {
		t.Fatalf("windups %+v, landings %+v; want one windup that lands on nobody", got.started, got.landed)
	}
}

// C12: standing inside a mob is never safe: every kind's attack reaches a player wholly inside it.
func TestEveryAttackReachesAPlayerInsideTheMob(t *testing.T) {
	for _, c := range []struct {
		mob     string
		attacks []int
	}{{"wild_boar", []int{0}}, {"green_slime", []int{0}}, {"orange_mushroom", []int{0}}, {"cave_bat", []int{0}}, {"king_slime", []int{0, 1}}} {
		t.Run(c.mob, func(t *testing.T) {
			a := alone(t, c.mob)
			p := standing(spawnX)
			got := play(a, 1, 120, still(p))
			var hit []int
			for _, n := range got.landedTicks() {
				for _, l := range got.landed[n] {
					if slices.ContainsFunc(l.Hits, func(h mobs.Hit) bool { return h.Player == p.ID }) && !slices.Contains(hit, l.Attack) {
						hit = append(hit, l.Attack)
					}
				}
			}
			slices.Sort(hit)
			if !slices.Equal(hit, c.attacks) {
				t.Fatalf("attacks %v landed on the player inside; want %v", hit, c.attacks)
			}
		})
	}
}

// C12: a slime's hop lands where it aimed as its windup began, a player's width short of its 72 px, and hits who's in its spread there.
func TestAHopLandsWhereItAimedAtTheWindup(t *testing.T) {
	a := alone(t, "green_slime")
	p := standing(250)
	got := play(a, 1, 21, still(p))
	if ticks := got.landedTicks(); !slices.Equal(ticks, []uint64{21}) || !slices.Equal(got.landed[21][0].Hits, []mobs.Hit{{Player: p.ID, Dir: 1}}) {
		t.Fatalf("landed %+v; want the hop on the player at tick 21, after its 600 ms windup and 400 ms travel", got.landed)
	}
	if v := a.Views()[0]; v.X != 250 {
		t.Fatalf("slime at x %v; want 250, on the player", v.X)
	}

	a = alone(t, "green_slime")
	got = play(a, 1, 21, func(n uint64) []mobs.Body {
		if n == 1 {
			return []mobs.Body{p}
		}
		return []mobs.Body{at(p, 330)}
	})
	if len(got.landed) != 0 || a.Views()[0].X != 250 {
		t.Fatalf("landed %+v with the slime at x %v; want it at 250 where it aimed, missing the player who left", got.landed, a.Views()[0].X)
	}
}

// C12: a mushroom's charge stops at the first player its box meets and lands on them only.
func TestAChargeStopsAtTheFirstPlayerItMeets(t *testing.T) {
	a := alone(t, "orange_mushroom") // 36 wide: its box 182-218
	near, far := standing(260), standing(300)
	got := play(a, 1, 40, still(near, far))
	ticks := got.landedTicks()
	if len(ticks) != 1 || !slices.Equal(got.landed[ticks[0]][0].Hits, []mobs.Hit{{Player: near.ID, Dir: 1}}) {
		t.Fatalf("landed %+v; want one landing, on the nearer player", got.landed)
	}
	if v := a.Views()[0]; v.X+18 != near.Box.X {
		t.Fatalf("mushroom at x %v; want its front at the nearer player's side, %v", v.X, near.Box.X)
	}
}

// C12: a charge that crosses a player within one tick's step still stops at them: it meets whoever its box sweeps over.
func TestAFastChargeStopsAtAPlayerItWouldStepPast(t *testing.T) {
	data := worldtest.LoadDataWith(t, map[string]func(string) string{"mobs.yaml": func(s string) string {
		return strings.Replace(s, "    attacks: [{use: charge}]\n", "    attacks: [{use: charge, travel: 50ms}]\n", 1) // the orange mushroom's, all in one tick
	}})
	m := slimes(time.Hour)
	m.Spawns[0].Mob, m.Spawns[0].Max = "orange_mushroom", 1
	a := mobs.NewArena(mobs.Deps{Map: m, Data: data, Roll: worldtest.FixedRoll{V: 0.5}}, mobs.Tuning{TickInterval: tick})
	p := standing(240) // their box 228-252: clear of the mushroom's at its start, 182-218, and at its step's end, 264-300
	got := play(a, 1, 40, still(p))
	if ticks := got.landedTicks(); len(ticks) != 1 || !slices.Equal(got.landed[ticks[0]][0].Hits, []mobs.Hit{{Player: p.ID, Dir: 1}}) {
		t.Fatalf("landed %+v; want the charge on the player it passed in one step", got.landed)
	}
	if v := a.Views()[0]; v.X+18 != p.Box.X {
		t.Fatalf("mushroom at x %v; want its front stopped at the player's side, %v", v.X, p.Box.X)
	}
}

// C12: a bat's spit flies from its front and lands on the first player in its path; one behind a solid, or past its range, it never reaches.
func TestASpitLandsOnTheFirstPlayerInItsPath(t *testing.T) {
	a := alone(t, "cave_bat") // 40 wide: its front at 220
	near, far := standing(300), standing(340)
	got := play(a, 1, 30, still(near, far))
	if ticks := got.landedTicks(); !slices.Equal(ticks, []uint64{17}) || !slices.Equal(got.landed[17][0].Hits, []mobs.Hit{{Player: near.ID, Dir: 1}}) {
		t.Fatalf("landed %+v; want the spit on the nearer player at tick 17: thrown at 13, 15 px a tick", got.landed)
	}

	walled := alone(t, "cave_bat", func(m *gamedata.Map) { m.Geometry.Solids = []model.Rect{{X: 260, Y: 300, W: 10, H: 100}} })
	if got := play(walled, 1, 50, still(near)); len(got.started) != 1 || len(got.landed) != 0 {
		t.Fatalf("windups %+v, landings %+v; want one spit, ending at the wall", got.started, got.landed)
	}

	a = alone(t, "cave_bat")
	got = play(a, 1, 50, func(n uint64) []mobs.Body {
		if n <= 12 {
			return []mobs.Body{near}
		}
		return []mobs.Body{at(near, 480)} // its box from 468, past the spit's 240 px from the bat's front
	})
	if len(got.started) != 1 || len(got.landed) != 0 {
		t.Fatalf("windups %+v, landings %+v; want one spit, ending at its range", got.started, got.landed)
	}
}

// C12: a boss takes its two attacks in turn, the hop and the slam, or whichever is ready when only one is.
func TestABossAlternatesItsAttacks(t *testing.T) {
	a := alone(t, "king_slime")
	got := play(a, 1, 140, still(standing(260)))
	var order [][2]uint64
	for _, s := range got.started {
		order = append(order, [2]uint64{s.Tick, uint64(s.Attack)})
	}
	// The hop is busy 1.3 s and ready every 2.5 s, the slam busy 1.6 s and ready every 5 s.
	if want := [][2]uint64{{1, 0}, {27, 1}, {59, 0}, {109, 0}, {135, 1}}; !slices.Equal(order, want) {
		t.Fatalf("attacks by tick %v; want %v", order, want)
	}
}

// C12: a mob a player hits follows them for the provoke's full 5 s, even out of its sight, then, never having seen them, lets them go at once and patrols.
func TestAProvokedMobFollowsItsAttackerThenPatrols(t *testing.T) {
	a := alone(t, "green_slime")
	p := standing(600) // 400 px off: out of its sight
	a.Step(1, []mobs.Body{p})
	s := meleeAt(spawnX-20, 1)
	s.By, s.Power = p.ID, model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 1, Accuracy: 100}
	if _, reached := one(t, a.Strike(s)); !reached {
		t.Fatal("the strike missed the slime")
	}
	play(a, 2, 101, still(p)) // the provoke's 100 turns
	if v := a.Views()[0]; v.X != maxX || v.Facing != 1 {
		t.Fatalf("slime %+v at tick 101; want it held at its right edge, x %v, still facing the player", v, maxX)
	}
	play(a, 102, 102, still(p))
	if v := a.Views()[0]; v.Facing != -1 {
		t.Fatalf("slime %+v at tick 102; want it patrolling, turned back at its edge, with no second kept for a player it never saw", v)
	}
}

// C12: a mob goes after a player within its sight, feet level to 96 px up, past walls, and holds where it can't reach them.
func TestAMobGoesAfterAPlayerItSees(t *testing.T) {
	wall := func(m *gamedata.Map) { m.Geometry.Solids = []model.Rect{{X: 100, Y: floor - 100, W: 10, H: 100}} }
	tests := []struct {
		name   string
		player mobs.Body
		facing int
		edits  []func(*gamedata.Map)
	}{
		{"level, 200 px to its left", standing(0), -1, nil},
		{"behind a wall to its left", standing(0), -1, []func(*gamedata.Map){wall}},
		{"96 px up to its left", up(standing(0), 96), -1, nil},
		{"97 px up to its left: unseen, it patrols on right", up(standing(0), 97), 1, nil},
		{"below it to its left: unseen", up(standing(0), -1), 1, nil},
		{"301 px to its left: unseen", standing(spawnX - 301), 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := alone(t, "green_slime", tt.edits...) // spawned facing right
			if got := play(a, 1, 10, still(tt.player)); len(got.started) != 0 {
				t.Fatalf("started %+v; want no attack", got.started)
			}
			if v := a.Views()[0]; v.Facing != tt.facing || v.X == spawnX {
				t.Fatalf("slime %+v; want it walking facing %d", v, tt.facing)
			}
		})
	}
	a := alone(t, "green_slime")
	play(a, 1, 100, still(standing(480)))
	if v := a.Views()[0]; v.X != maxX || v.Facing != 1 {
		t.Fatalf("slime %+v; want it held at its right edge, its hop short of the player", v)
	}
	if got := play(a, 101, 101, still(standing(300))); len(got.started) != 1 {
		t.Fatalf("started %+v; want a hop at the player come into its reach", got.started)
	}
}

// C12: a mob keeps the player it went after while they're out of its sight for under 1 s, turning after them, and then lets them go.
func TestAMobKeepsItsTargetASecondOutOfSight(t *testing.T) {
	a := alone(t, "green_slime")
	p := standing(450)
	play(a, 1, 10, still(p)) // last seen at tick 10
	play(a, 11, 25, still(up(at(p, 0), 97)))
	if v := a.Views()[0]; v.Facing != -1 {
		t.Fatalf("slime %+v; want it turned after the unseen player on its left", v)
	}
	play(a, 26, 29, still(up(at(p, 450), 97)))
	if v := a.Views()[0]; v.Facing != 1 {
		t.Fatalf("slime %+v; want it turned after them again at tick 29, 19 ticks unseen", v)
	}
	play(a, 30, 40, still(up(at(p, 0), 97)))
	if v := a.Views()[0]; v.Facing != 1 {
		t.Fatalf("slime %+v; want it patrolling on right, the player let go at tick 30", v)
	}
}

// up is p lifted by dy, its feet that much above the floor.
func up(p mobs.Body, dy float64) mobs.Body {
	p.Box.Y -= dy
	return p
}

// C12: a mob killed this tick lands nothing, and its spit in flight goes with it.
func TestAKilledMobLandsNothing(t *testing.T) {
	a := alone(t, "wild_boar")
	p := standing(240)
	play(a, 1, 12, still(p))
	a.Step(13, []mobs.Body{p})
	if len(a.Landings()) != 1 {
		t.Fatalf("landings %+v; want the bite", a.Landings())
	}
	if _, reached := one(t, a.Strike(meleeAt(spawnX-20, 1))); !reached {
		t.Fatal("the strike missed the boar")
	}
	if len(a.Landings()) != 0 {
		t.Fatalf("landings %+v after the boar died; want none", a.Landings())
	}

	a = alone(t, "cave_bat")
	play(a, 1, 13, still(standing(300)))
	if _, reached := one(t, a.Strike(meleeAt(spawnX-20, 1))); !reached {
		t.Fatal("the strike missed the bat")
	}
	if got := play(a, 14, 30, still(standing(300))); len(got.landed) != 0 {
		t.Fatalf("landings %+v; want the dead bat's spit gone", got.landed)
	}
}

// C12: mobs in a town never attack.
func TestMobsInATownNeverAttack(t *testing.T) {
	a := townArena(t, 0.5)
	if got := play(a, 1, 100, still(standing(spawnX))); len(got.started) != 0 || len(got.landed) != 0 {
		t.Fatalf("windups %+v, landings %+v in a town", got.started, got.landed)
	}
}

// A mob's attack meets the defense of its damage type: a slime's physical, a bat's magic.
func TestAnAttackMeetsTheDefenseOfTheMobsDamageType(t *testing.T) {
	hit := func(mob string, d model.Defender) int {
		t.Helper()
		a := alone(t, mob)
		p := standing(spawnX)
		got := play(a, 1, 30, still(p))
		ticks := got.landedTicks()
		if len(ticks) == 0 {
			t.Fatalf("%s landed nothing", mob)
		}
		dealt := a.Roll(got.landed[ticks[0]][0], p.ID, d)
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

// C12: a mob walks on toward its target while its attack cools down, until it stops at them.
func TestAMobClosesInWhileItsAttackCools(t *testing.T) {
	a := alone(t, "wild_boar") // 56 px wide at 200: its right edge at 228
	p := standing(270)         // their left edge at 258: 30 px off, past the bite's 24
	got := play(a, 1, 40, still(p))
	if len(got.started) != 1 || got.started[0].Tick != 3 {
		t.Fatalf("started %+v; want the bite once it had walked into reach, at tick 3", got.started)
	}
	if v := a.Views()[0]; v.X != 258-28 {
		t.Fatalf("boar %+v; want it walked on to the player while the bite cooled, at x %v", v, 258-28)
	}
}

// C12: a mob that shoots holds once its target is in its shot's area, and walks closer only until then.
func TestAShooterHoldsAtRange(t *testing.T) {
	for _, tt := range []struct {
		name   string
		player float64
		from   uint64
	}{
		{"in range from the start", 420, 1},
		{"20 px past its range", 480, 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := alone(t, "cave_bat") // 40 px wide at 200: its spit's front reaches 460
			got := play(a, 1, 40, still(standing(tt.player)))
			if len(got.started) == 0 || got.started[0].Tick != tt.from {
				t.Fatalf("started %+v; want the first spit at tick %d", got.started, tt.from)
			}
			x := a.Views()[0].X
			play(a, 41, 60, still(standing(tt.player)))
			if v := a.Views()[0]; v.X != x || v.X > 210 {
				t.Fatalf("bat %+v; want it holding at range, short of x 210", v)
			}
		})
	}
}

// C12: a mob goes after its target, its provoker first, and attacks another player only when it can't get closer.
func TestAMobAttacksWhoItCanReachOnlyWhenStuck(t *testing.T) {
	far, near := standing(450), standing(170) // near's right edge 10 px into the slime's hop, held to its ground
	a := alone(t, "green_slime", pinned)
	play(a, 1, 5, still(far)) // its target: the only player it sees
	if got := play(a, 6, 6, still(far, near)); len(got.started) != 1 || got.started[0].Facing != -1 {
		t.Fatalf("started %+v; want a hop at the player in reach on its left, its target out of reach and it pinned", got.started)
	}

	a = alone(t, "green_slime")
	play(a, 1, 1, still(far))
	s := meleeAt(spawnX-20, 1)
	s.By, s.Power = far.ID, model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 1, Accuracy: 0} // a MISS provokes too
	a.Strike(s)
	if got := play(a, 2, 30, still(far, standing(spawnX-30))); len(got.started) != 0 || a.Views()[0].Facing != 1 || a.Views()[0].X <= spawnX {
		t.Fatalf("started %+v, slime %+v; want it walking off after its provoker, no hop at the nearer player", got.started, a.Views()[0])
	}

	a = alone(t, "green_slime")
	play(a, 1, 1, still(far))
	a.Strike(s)
	wall := standing(spawnX + 16 + 12) // against its right side, between it and its provoker
	if got := play(a, 2, 2, still(far, wall)); len(got.started) != 1 || got.started[0].Facing != 1 {
		t.Fatalf("started %+v; want a hop at the player in its way, its provoker beyond them", got.started)
	}
}

// A mob within a step of its target stops at their centre rather than stepping past and back, its facing kept.
func TestAMobStopsAtItsTargetsCentre(t *testing.T) {
	a := alone(t, "green_slime") // 1.6 px a tick
	p := up(standing(spawnX+1), 88)
	play(a, 1, 1, still(p))
	if v := a.Views()[0]; v.X != spawnX+1 || v.Facing != 1 {
		t.Fatalf("slime %+v; want it stopped at the player's centre, x %v, facing right", v, spawnX+1)
	}
}

// A mob under a target it can't reach, 88 px up, stands still beneath them, keeping its x and its facing tick after tick.
func TestAMobUnderItsTargetStandsStill(t *testing.T) {
	a := alone(t, "green_slime")
	p := up(standing(spawnX-40), 88)
	play(a, 1, 30, still(p)) // 40 px at 1.6 a tick: there by tick 25
	for n := uint64(31); n <= 45; n++ {
		play(a, n, n, still(p))
		if v := a.Views()[0]; v.X != spawnX-40 || v.Facing != -1 {
			t.Fatalf("tick %d: slime %+v; want it still at x %v, facing left", n, v, spawnX-40)
		}
	}
}

// A mob already under its target can't step closer, so it hops at another player its ready hop reaches.
func TestAMobUnderItsTargetAttacksWhoItCanReach(t *testing.T) {
	a := alone(t, "green_slime")
	p := up(standing(spawnX-40), 88)
	play(a, 1, 30, still(p))           // its target, beneath them by tick 25
	near := standing(spawnX - 40 + 48) // on its ground, 48 px to its right: within the hop's 72
	if got := play(a, 31, 31, still(p, near)); len(got.started) != 1 || got.started[0].Facing != 1 {
		t.Fatalf("started %+v; want a hop at the player beside it, its target out of reach above", got.started)
	}
}
