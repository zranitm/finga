package mobs_test

import (
	"math"
	"slices"
	"testing"
	"time"

	"finga/internal/application/world/mobs"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

func ticksOf(d time.Duration) uint64 { return model.TicksIn(d, tick) }

// tenAttack is the attack damage over time takes its power of in these tests.
var tenAttack = model.Attacker{Damage: gamedata.Physical, PhysicalAttack: 10}

// inflicting is s with status id lasting d, by its striker; damage over time ticks power × tenAttack.
func inflicting(t *testing.T, s mobs.Strike, id string, d time.Duration, power float64) mobs.Strike {
	t.Helper()
	def, ok := worldtest.LoadData(t).Status(id)
	if !ok {
		t.Fatalf("no status %s", id)
	}
	s.Inflicts = append(s.Inflicts, model.InflictionOf(def, gamedata.Inflict{Status: id, Time: d, Power: power}, model.EntityRef{Player: s.By}, tenAttack, ticksOf))
	return s
}

// statusSkill is a status skill by a player at (x, z): every mob within 10 m.
func statusSkill(x, z float64) mobs.Strike {
	return mobs.Strike{By: kernel.NewCharacterID(), Shape: gamedata.Shape{Kind: gamedata.AreaShape, Radius: 10}, Most: math.MaxInt, Aim: model.Aim{X: x, Z: z}}
}

// knocks is a status skill knocking down the mobs near (x, z), to see whether a hard status takes.
func knocks(t *testing.T, x, z float64) mobs.Strike {
	return inflicting(t, statusSkill(x, z), gamedata.Knockdown, 0, 0)
}

// A stun leaves a mob where it stands, its windup ended, not even turning to whoever comes, for its time, its view showing it; then it comes on again.
func TestAStunHoldsAMobStillAndEndsItsWindup(t *testing.T) {
	a := alone(t, "green_slime")
	v := theMob(t, a)
	p := player(20, v.Z-3) // in its hop's reach
	at, next := windup(t, a, []mobs.Body{p}, 1)
	one(t, a.Strike(inflicting(t, nudge(20, v.Z+1), "stun", time.Second, 0)))
	stunned := theMob(t, a)
	if !slices.Equal(stunned.Statuses, []model.StatusView{{ID: "stun", Ends: next + 20}}) {
		t.Fatalf("statuses %+v; want the stun to tick %d", stunned.Statuses, next+20)
	}
	east := player(23, v.Z)                    // where it doesn't face
	got := stepThrough(a, next, next+19, east) // the stun's 20 ticks from the next
	if len(got.started)+len(got.landed) != 0 {
		t.Fatalf("started %v, landed at %v; want nothing while stunned", got.startTicks(), got.landTicks)
	}
	if now := theMob(t, a); now.X != stunned.X || now.Z != stunned.Z || now.Yaw != stunned.Yaw {
		t.Fatalf("slime %+v while stunned as %+v; want it still, facing as it was", now, stunned)
	}
	if got := stepThrough(a, next+20, at.Tick+60, p); len(got.started) == 0 {
		t.Fatal("the slime never hopped again once its stun ended")
	}
	if now := theMob(t, a); len(now.Statuses) != 0 {
		t.Fatalf("statuses %+v; want none once the stun ended", now.Statuses)
	}
}

// A pin keeps a mob where it stands, so it neither chases nor leaps, but it still bites whoever it reaches.
func TestAPinHoldsAMobButNotItsBite(t *testing.T) {
	boar := alone(t, "wild_boar")
	v := theMob(t, boar)
	far := player(20, v.Z-4)
	boar.Afflict(inflicting(t, statusSkill(20, v.Z-4), "pin", 2*time.Second, 0))
	if got := stepThrough(boar, 1, 20, far); len(got.started) != 0 || theMob(t, boar).Z != v.Z {
		t.Fatalf("pinned boar at z %v, started %v; want it still, out of its bite's reach", theMob(t, boar).Z, got.startTicks())
	}
	near := player(20, v.Z-0.875-playerRadius) // touching it
	if got := stepThrough(boar, 21, 30, near); len(got.started) != 1 || theMob(t, boar).Z != v.Z {
		t.Fatalf("pinned boar at z %v, started %v; want its bite on the player it touches, where it stands", theMob(t, boar).Z, got.startTicks())
	}
	slime := alone(t, "green_slime")
	sv := theMob(t, slime)
	slime.Afflict(inflicting(t, statusSkill(20, sv.Z-3), "pin", 2*time.Second, 0))
	if got := stepThrough(slime, 1, 30, player(20, sv.Z-3)); len(got.started) != 0 {
		t.Fatalf("a pinned slime started %v; want no hop, a leap moving it", got.startTicks())
	}
}

// A slow takes its factor off a mob's walk.
func TestASlowedMobWalksAtItsFactor(t *testing.T) {
	walked := func(slow bool) float64 {
		a := alone(t, "green_slime")
		v := theMob(t, a)
		p := player(20, v.Z-8) // in its sight, out of its hop's reach
		if slow {
			a.Afflict(inflicting(t, statusSkill(20, v.Z-8), "slow", 5*time.Second, 0))
		}
		stepThrough(a, 1, 10, p)
		return v.Z - theMob(t, a).Z
	}
	free, slowed := walked(false), walked(true)
	if free <= 0 || math.Abs(slowed-0.6*free) > 1e-9 {
		t.Fatalf("walked %v slowed against %v; want 0.6 of it", slowed, free)
	}
}

// Super armor shrugs off a hard status, and the rest last half their time: a 4 s poison ticks twice.
func TestSuperArmorShrugsOffHardStatusesAndHalvesTheRest(t *testing.T) {
	a := alone(t, "mossback_snail")
	v := theMob(t, a)
	p := player(20, v.Z-4) // in its charge's reach
	_, next := windup(t, a, []mobs.Body{p}, 1)
	a.Afflict(inflicting(t, inflicting(t, statusSkill(20, v.Z-4), "stun", 5*time.Second, 0), "poison", 4*time.Second, 1))
	hits, ticks := 0, 0
	for n := next; n < next+120; n++ {
		a.Step(n, []mobs.Body{p})
		for _, l := range a.Landings() {
			hits += len(l.Hits)
		}
		ticks += len(a.Dots())
	}
	if hits < 1 {
		t.Fatal("its charge never landed through the stun")
	}
	if ticks != 2 {
		t.Fatalf("a 4 s poison ticked %d times on super armor; want 2, in its 2 s", ticks)
	}
}

// After a hard status ends, by its time, a shatter or waking, another takes on the next tick: nothing keeps it off.
func TestAHardStatusTakesAgainOnceOneEnds(t *testing.T) {
	for _, c := range []struct {
		name  string
		start func(t *testing.T, a *mobs.Arena, x, z float64)
		ended uint64 // the tick the status ends
	}{
		{"by its time", func(t *testing.T, a *mobs.Arena, x, z float64) {
			a.Afflict(inflicting(t, statusSkill(x, z), "stun", 500*time.Millisecond, 0)) // from tick 2 to 12
		}, 12},
		{"by a shatter", func(t *testing.T, a *mobs.Arena, x, z float64) {
			a.Afflict(inflicting(t, statusSkill(x, z), "freeze", 5*time.Second, 0))
			stepThrough(a, 2, 9)
			one(t, a.Strike(nudge(x, z+1))) // tick 9's hit shatters it
		}, 9},
		{"by waking", func(t *testing.T, a *mobs.Arena, x, z float64) {
			a.Afflict(inflicting(t, statusSkill(x, z), "sleep", 5*time.Second, 0))
			stepThrough(a, 2, 9)
			one(t, a.Strike(nudge(x, z+1))) // tick 9's hit wakes it
		}, 9},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := alone(t, "green_slime")
			v := theMob(t, a)
			stepThrough(a, 1, 1)
			c.start(t, a, v.X, v.Z)
			stepThrough(a, 2, c.ended+1)
			a.Afflict(knocks(t, v.X, v.Z))
			if got := theMob(t, a); got.Pose != model.Down {
				t.Fatalf("slime %+v; want it knocked down the tick after its status ended", got)
			}
		})
	}
}

// A freeze's shattering hit deals 1.5 times its damage.
func TestAShatterHitsHarder(t *testing.T) {
	hit := func(frozen bool) int {
		a := alone(t, "green_slime")
		v := theMob(t, a)
		if frozen {
			a.Afflict(inflicting(t, statusSkill(v.X, v.Z), "freeze", 5*time.Second, 0))
		}
		s := nudge(v.X, v.Z+1)
		s.Power.PhysicalAttack = 20
		b, _ := one(t, a.Strike(s))
		return b.Damage.Amount
	}
	plain, shattered := hit(false), hit(true)
	if want := int(math.Round(float64(plain) * 1.5)); plain < 2 || shattered != want {
		t.Fatalf("a shattering hit dealt %d against %d; want %d", shattered, plain, want)
	}
}

// The down-hits knockdown is the knockdown status, and takes right after a stun ends: the third blow knocks down.
func TestTheDownHitsKnockdownTakesRightAfterAStun(t *testing.T) {
	a := alone(t, "green_slime")
	v := theMob(t, a)
	a.Afflict(inflicting(t, statusSkill(v.X, v.Z), "stun", 500*time.Millisecond, 0)) // from tick 1 to 11
	stepThrough(a, 1, 11)
	for _, n := range []uint64{12, 20, 28} { // each in the down window of the last, each flinch over before the next
		stepThrough(a, n, n)
		if got := theMob(t, a); got.Pose != model.Standing {
			t.Fatalf("slime %+v at tick %d; want it standing before the blow", got, n)
		}
		nudgeFromNorth(t, a)
	}
	if got := theMob(t, a); got.Pose != model.Down {
		t.Fatalf("slime %+v; want the third blow to knock it down", got)
	}
}

// A skill's push moves a mob its distance over 0.2 s, stopped by a solid, then a flinch; super armor isn't pushed.
func TestASkillsPushMovesAMobOverAFifthOfASecond(t *testing.T) {
	a := alone(t, "green_slime")
	v := theMob(t, a)
	s := nudge(v.X, v.Z-1) // from the south: north
	s.Push = 2
	one(t, a.Strike(s))
	if got := theMob(t, a); got.Z != v.Z || got.Pose != model.Flinching {
		t.Fatalf("slime %+v; want it flinching, not moved yet", got)
	}
	for k, n := range []uint64{1, 2, 3, 4} {
		stepThrough(a, n, n)
		if got := theMob(t, a); math.Abs(got.Z-(v.Z+0.5*float64(k+1))) > 1e-9 {
			t.Fatalf("tick %d: slime at z %v, want %v", n, got.Z, v.Z+0.5*float64(k+1))
		}
	}
	stepThrough(a, 5, 10)
	if got := theMob(t, a); got.Pose != model.Flinching || math.Abs(got.Z-(v.Z+2)) > 1e-9 {
		t.Fatalf("slime %+v; want it 2 m north, flinching after the push", got)
	}
	stepThrough(a, 11, 11)
	if got := theMob(t, a); got.Pose != model.Standing {
		t.Fatalf("slime %+v; want the flinch over 300 ms after the push", got)
	}

	walled := newArena(t, meadow(t, open(sim.Box{X: 20, Z: 21.5, HalfW: 2, HalfD: 0.5, UX: 1, Top: 3}), spawn("green_slime", 1, 20, 20, 0.01)), worldtest.FixedRoll{V: 0.5})
	w := theMob(t, walled)
	s = nudge(w.X, w.Z-1)
	s.Push = 2
	one(t, walled.Strike(s))
	stepThrough(walled, 1, 4)
	if got := theMob(t, walled); math.Abs(got.Z-(21-0.5)) > 1e-6 {
		t.Fatalf("slime at z %v; want it stopped against the wall at %v", got.Z, 21-0.5)
	}

	snail := alone(t, "mossback_snail")
	sv := theMob(t, snail)
	s = nudge(sv.X, sv.Z-1)
	s.Push = 2
	snail.Afflict(s)
	stepThrough(snail, 1, 4)
	if got := theMob(t, snail); got.Z != sv.Z {
		t.Fatalf("super armor pushed to z %v from %v", got.Z, sv.Z)
	}
}

// A taunt makes its inflicter the mob's target at once, and holds it there: another's blows don't take it back.
func TestATauntTakesTheMobAndHoldsIt(t *testing.T) {
	a := alone(t, "green_slime")
	v := theMob(t, a)
	near, taunter := player(20, v.Z-4), player(27, v.Z) // near is nearer; the taunter east
	stepThrough(a, 1, 5, near, taunter)
	before := theMob(t, a)
	if before.Z >= v.Z {
		t.Fatalf("slime at z %v; want it after the nearer player, south", before.Z)
	}
	taunt := inflicting(t, statusSkill(taunter.X, taunter.Z), gamedata.Taunt, 5*time.Second, 0)
	taunt.By = taunter.ID
	taunt.Inflicts[0].By = model.EntityRef{Player: taunter.ID}
	a.Afflict(taunt)
	blow := nudge(near.X, near.Z) // the nearer one hits it from range
	blow.By, blow.Shape.Radius = near.ID, 10
	one(t, a.Strike(blow))
	stepThrough(a, 6, 70, near, taunter) // past the second a hit takes to win a target over
	if got := theMob(t, a); got.X <= before.X+1.5 || got.Z < before.Z-0.5 {
		t.Fatalf("slime at (%v, %v) from (%v, %v); want it gone east after the taunter all along", got.X, got.Z, before.X, before.Z)
	}
}

// Losing sight of a player drops them, and the mob sees them no more for its time, nor follows their blows.
func TestLosingSightDropsTheInflicter(t *testing.T) {
	a := alone(t, "green_slime")
	v := theMob(t, a)
	smoker, other := player(20, v.Z-4), player(27, v.Z)
	stepThrough(a, 1, 5, smoker, other)
	before := theMob(t, a)
	smoke := inflicting(t, statusSkill(smoker.X, smoker.Z), gamedata.LoseSight, 3*time.Second, 0)
	smoke.By = smoker.ID
	smoke.Inflicts[0].By = model.EntityRef{Player: smoker.ID}
	a.Afflict(smoke)
	blow := nudge(smoker.X, smoker.Z)
	blow.By, blow.Shape.Radius = smoker.ID, 10
	one(t, a.Strike(blow))
	stepThrough(a, 6, 40, smoker, other)
	if got := theMob(t, a); got.X <= before.X+0.5 {
		t.Fatalf("slime at x %v from %v; want it after the one it still sees, east", got.X, before.X)
	}
}

// Damage over time deals its steady damage each second, as its inflicter's, beside another's of the same status; a kill comes with it; a leaving inflicter's ends.
// A damage-over-time tick wakes a sleeping mob, as any damage does, and the sleep alone ends.
func TestDamageOverTimeWakesASleeper(t *testing.T) {
	a := alone(t, "green_slime")
	v := theMob(t, a)
	a.Afflict(inflicting(t, inflicting(t, statusSkill(v.X, v.Z), "sleep", 10*time.Second, 0), "poison", 4*time.Second, 0.2)) // its first tick at 21
	stepThrough(a, 1, 20)
	if got := theMob(t, a).Statuses; len(got) != 2 {
		t.Fatalf("statuses %+v before the poison's first tick; want the sleep and the poison", got)
	}
	stepThrough(a, 21, 21)
	if got := theMob(t, a).Statuses; !slices.Equal(got, []model.StatusView{{ID: "poison", Ends: 81}}) {
		t.Fatalf("statuses %+v after the poison's first tick; want the poison alone, the sleep woken", got)
	}
}

func TestDamageOverTimeIsItsInflicters(t *testing.T) {
	a := alone(t, "green_slime")
	v := theMob(t, a)
	mine, theirs := inflicting(t, statusSkill(v.X, v.Z), "poison", 4*time.Second, 0.2), inflicting(t, statusSkill(v.X, v.Z), "poison", 4*time.Second, 0.1) // 2 and 1 a tick, short of the slime's 34 HP
	a.Afflict(mine)
	stepThrough(a, 1, 5)
	a.Afflict(theirs)
	slime, _ := worldtest.LoadData(t).Mob("green_slime")
	want := map[kernel.CharacterID]int{mine.By: model.Steady(tenAttack.Times(0.2), model.MobDefender(slime)), theirs.By: model.Steady(tenAttack.Times(0.1), model.MobDefender(slime))}
	got := map[kernel.CharacterID][]uint64{}
	for n := uint64(6); n <= 120; n++ {
		a.Step(n, nil)
		for _, b := range a.Dots() {
			if b.Damage.Amount != want[b.Damage.Source.Player] || b.Damage.Crit || b.Damage.Miss || b.Damage.Target.Mob != v.ID || b.Damage.Status != "poison" {
				t.Fatalf("tick %d: %+v; want %d", n, b.Damage, want[b.Damage.Source.Player])
			}
			got[b.Damage.Source.Player] = append(got[b.Damage.Source.Player], n)
		}
		if n == 30 {
			a.DropBy(theirs.By)
		}
	}
	if len(got[mine.By]) != 4 || got[mine.By][0] != 21 || got[mine.By][3] != 81 || len(got[theirs.By]) != 1 || got[theirs.By][0] != 26 || want[mine.By] == want[theirs.By] {
		t.Fatalf("ticked at %v; want mine 4 from tick 21, theirs once at 26 before they left", got)
	}

	b := alone(t, "green_slime")
	bv := theMob(t, b)
	b.Afflict(inflicting(t, statusSkill(bv.X, bv.Z), "poison", 4*time.Second, 1000))
	for n := uint64(1); n <= 21; n++ {
		b.Step(n, nil)
		if dots := b.Dots(); len(dots) > 0 {
			if k := dots[0].Killed; k == nil || k.Mob.ID != "green_slime" || len(b.Views()) != 0 {
				t.Fatalf("dots %+v, mobs %d; want the poison's kill, the slime gone", dots, len(b.Views()))
			}
			return
		}
	}
	t.Fatal("the poison never ticked")
}

// A bleed's tick doubles while its mob moves.
func TestABleedHurtsMoreOnTheMove(t *testing.T) {
	bled := func(moving bool) int {
		a := alone(t, "green_slime")
		v := theMob(t, a)
		var players []mobs.Body
		if moving {
			players = []mobs.Body{player(20, v.Z-8)} // it chases them
		}
		a.Afflict(inflicting(t, statusSkill(v.X, v.Z), "bleed", 3*time.Second, 1))
		for n := uint64(1); n <= 21; n++ {
			a.Step(n, players)
			if dots := a.Dots(); len(dots) == 1 {
				return dots[0].Damage.Amount
			}
		}
		t.Fatal("the bleed never ticked")
		return 0
	}
	still, moving := bled(false), bled(true)
	slime, _ := worldtest.LoadData(t).Mob("green_slime")
	if want := model.Steady(tenAttack.Times(2), model.MobDefender(slime)); moving != want || still >= moving {
		t.Fatalf("bled %d still and %d moving; want %d on the move", still, moving, want)
	}
}

// Weaken and blind take from a mob's blows, armour break from its defense.
func TestStatusesWeakenAMobsBlowsAndBreakItsArmour(t *testing.T) {
	rolled := func(id string, d model.Defender) model.Damage {
		a := alone(t, "green_slime")
		v := theMob(t, a)
		p := player(20, v.Z-3)
		if id != "" {
			a.Afflict(inflicting(t, statusSkill(v.X, v.Z), id, 30*time.Second, 0))
		}
		_, next := windup(t, a, []mobs.Body{p}, 1)
		for n := next; n < next+40; n++ {
			a.Step(n, []mobs.Body{p})
			if ls := a.Landings(); len(ls) > 0 {
				return a.Roll(ls[0], p.ID, d)
			}
		}
		t.Fatal("no landing")
		return model.Damage{}
	}
	plain, weak := rolled("", model.Defender{}), rolled("weaken", model.Defender{})
	if want := int(math.Round(float64(plain.Amount) * 0.75)); weak.Amount != want && weak.Amount != want-1 && weak.Amount != want+1 {
		t.Fatalf("weakened %d against %d; want three quarters", weak.Amount, plain.Amount)
	}
	// At a roll of 0.5 against 40 evasion, the slime's 24 accuracy lands (63%) and blind's 12 misses (32%).
	if seen, blind := rolled("", model.Defender{Evasion: 40}), rolled("blind", model.Defender{Evasion: 40}); seen.Miss || !blind.Miss {
		t.Fatalf("seeing %+v, blind %+v; want blind to miss what seeing lands", seen, blind)
	}

	struck := func(broken bool) int {
		a := alone(t, "green_slime")
		v := theMob(t, a)
		if broken {
			a.Afflict(inflicting(t, statusSkill(v.X, v.Z), "armor_break", 30*time.Second, 0))
		}
		s := nudge(v.X, v.Z+1)
		s.Power.PhysicalAttack = 1000
		b, _ := one(t, a.Strike(s))
		return b.Damage.Amount
	}
	slime, _ := worldtest.LoadData(t).Mob("green_slime")
	if whole, broken := struck(false), struck(true); broken <= whole || slime.PhysicalDefense == 0 {
		t.Fatalf("struck %d broken against %d whole; want more through the broken armour", broken, whole)
	}
}

// A status skill lands on every mob in its shape without a roll, and provokes them.
func TestAStatusSkillAlwaysLands(t *testing.T) {
	a := newArena(t, meadow(t, open(), spawn("green_slime", 2, 20, 20, 2)), worldtest.FixedRoll{V: 0.999}) // a roll every hit misses
	a.Afflict(knocks(t, 20, 20))
	for _, v := range a.Views() {
		if v.Pose != model.Down {
			t.Fatalf("slime %+v; want both knocked down", v)
		}
	}
}
