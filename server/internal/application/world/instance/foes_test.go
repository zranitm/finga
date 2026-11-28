package instance_test

import (
	"context"
	"math"
	"testing"
	"testing/synctest"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/internal/shared/quant"
	"finga/internal/shared/sim"
)

// apart is how far apart two players' bodies' centres stand, as the latest frames have them.
func apart(a, b *worldtest.Sink, ida, idb kernel.CharacterID) float64 {
	va, _ := a.Own(ida)
	vb, _ := b.Own(idb)
	return math.Hypot(va.State.X-vb.State.X, va.State.Z-vb.State.Z)
}

// pressInto has id press toward the other's z for ticks, its view the latest frame, and returns the inputs' last seq.
func pressInto(inst *instance.Instance, s *worldtest.Sink, id kernel.CharacterID, seq uint32, up bool, ticks int) uint32 {
	return worldtest.Walk(inst, id, seq, sim.Input{Up: up, Down: !up}, ticks)
}

// pvp.md section 19: in a match a living enemy's body blocks a fighter as a mob's does, so one walking up to an enemy stops with their centres 0.75 m apart, in reach.
func TestAFighterWalkingIntoAnEnemyStopsAtTheirBody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := pvpData(t, "1.0", "1.0")
		inst, ayla, robin, as, rs, stop := twoFighters(t, data, nil)
		defer stop()
		a, _ := as.Own(ayla.ID)
		r, _ := rs.Own(robin.ID)
		up := r.State.Z > a.State.Z
		pressInto(inst, as, ayla.ID, 0, up, 200)
		worldtest.Walk(inst, ayla.ID, 400, sim.Input{}, 5)
		if d := apart(as, rs, ayla.ID, robin.ID); d < 0.74 || d > 0.76 {
			t.Fatalf("Ayla walked into Robin and stands %.3f m from his centre; want 0.75", d)
		}
	})
}

// Teammates and everyone outside a match pass through each other.
func TestTeammatesAndPlayersOutsideAMatchWalkThroughEachOther(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, plain(t, nil, nil), nil, worldtest.Options{})
		defer stop()
		a, b := worldtest.NewPlayer("Ayla", 100, 100), worldtest.NewPlayer("Robin", 104, 100)
		as, bs := join(t, inst, a), join(t, inst, b)
		worldtest.Walk(inst, a.ID, 0, sim.Input{Right: true}, 80)
		va, _ := as.Own(a.ID)
		vb, _ := bs.Own(b.ID)
		if va.State.X < vb.State.X+1 {
			t.Fatalf("Ayla at x %.2f after walking east through Robin at %.2f; want her past him", va.State.X, vb.State.X)
		}
	})
}

// A downed enemy doesn't block, as a dead mob doesn't.
func TestADownedEnemyDoesNotBlock(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := pvpData(t, "1.0", "1.0")
		inst, ayla, robin, as, rs, stop := twoFighters(t, data, nil)
		defer stop()
		r, _ := rs.Own(robin.ID)
		a, _ := as.Own(ayla.ID)
		seq := walkTo(inst, as, ayla.ID, 0, r.State.Z-1.2*sign(r.State.Z-a.State.Z), 0.2)
		for range 1200 {
			_ = inst.Attack(ayla.ID, 0, model.EntityRef{Player: robin.ID})
			seq = worldtest.Walk(inst, ayla.ID, seq, sim.Input{}, 1)
			if own, _ := rs.Own(robin.ID); own.Dead {
				break
			}
		}
		down, _ := rs.Own(robin.ID)
		if !down.Dead {
			t.Fatal("Robin never went down")
		}
		pressInto(inst, as, ayla.ID, seq, r.State.Z > a.State.Z, 60)
		after, _ := as.Own(ayla.ID)
		if sign(after.State.Z-a.State.Z) != sign(r.State.Z-a.State.Z) || math.Abs(after.State.Z-down.State.Z) > 0.3 && math.Abs(after.State.Z-a.State.Z) < math.Abs(down.State.Z-a.State.Z) {
			t.Fatalf("Ayla at z %.2f after walking on over Robin down at %.2f", after.State.Z, down.State.Z)
		}
		if d := math.Abs(after.State.Z - down.State.Z); d < 0.5 && math.Abs(after.State.Z-a.State.Z) <= math.Abs(down.State.Z-a.State.Z) {
			t.Fatalf("Ayla stopped %.2f m from Robin's body; a downed one doesn't block", d)
		}
	})
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}

// A fighter held through a lost connection keeps their body: it blocks.
func TestAHeldBodyBlocks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := pvpData(t, "1.0", "1.0")
		inst, ayla, robin, as, rs, stop := twoFighters(t, data, nil)
		defer stop()
		if held, err := inst.Hold(context.Background(), robin.ID, robin.Connection); err != nil || !held {
			t.Fatalf("hold: %v, %v", held, err)
		}
		a, _ := as.Own(ayla.ID)
		r, _ := rs.Own(robin.ID)
		pressInto(inst, as, ayla.ID, 0, r.State.Z > a.State.Z, 200)
		worldtest.Walk(inst, ayla.ID, 400, sim.Input{}, 5)
		if d := apart(as, rs, ayla.ID, robin.ID); d < 0.74 || d > 0.76 {
			t.Fatalf("Ayla walked into Robin's held body and stands %.3f m from it; want 0.75", d)
		}
	})
}

// Teammates in a match pass through each other: Ayla walks on past Bea's body.
func TestTeammatesInAMatchWalkThroughEachOther(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := pvpData(t, "1.0", "1.0")
		ayla, bea, robin := fighterFrom(t, data, "Ayla"), fighterFrom(t, data, "Bea"), fighterFrom(t, data, "Robin")
		spec := model.MatchSpec{ID: kernel.NewMatchID(), Mode: "duel", Sides: [2][]model.Fighter{{{Character: ayla.ID}, {Character: bea.ID}}, {{Character: robin.ID}}}}
		inst, stop := worldtest.RunMatch(t, data, spec, nil, worldtest.Options{})
		defer stop()
		sinks := joinAll(t, inst, ayla, bea, robin)
		sleep(data.Pvp().Rounds.Prep + data.Pvp().Rounds.Countdown + worldtest.TickInterval)
		a, _ := sinks[ayla.ID].Own(ayla.ID)
		b, _ := sinks[bea.ID].Own(bea.ID)
		dx, dz := b.State.X-a.State.X, b.State.Z-a.State.Z
		in := sim.Input{Right: dx > 0, Left: dx < 0}
		along := func(v model.PlayerView) float64 { return v.State.X }
		if math.Abs(dz) > math.Abs(dx) {
			in, along = sim.Input{Up: dz > 0, Down: dz < 0}, func(v model.PlayerView) float64 { return v.State.Z }
		}
		worldtest.Walk(inst, ayla.ID, 0, in, 80)
		got, _ := sinks[ayla.ID].Own(ayla.ID)
		if past := along(got) - along(b); math.Abs(past) < 0.5 || (past > 0) != (along(b) > along(a)) {
			t.Fatalf("Ayla walked toward Bea and stands %.2f from her along the line; want her through and past", past)
		}
	})
}

// headOn has the two press toward each other for ticks, each input's view lag ticks behind the newest frame, as screens that drew the other late.
func headOn(inst *instance.Instance, as, rs *worldtest.Sink, a, b kernel.CharacterID, lag, ticks int) {
	va, _ := as.Own(a)
	vb, _ := rs.Own(b)
	up := vb.State.Z > va.State.Z
	var seq uint32
	for range ticks {
		seq++
		view := uint32(max(int(as.Last().Tick)-lag, 0))
		_ = inst.Input(a, seq, sim.Input{Up: up, Down: !up}, view)
		_ = inst.Input(b, seq, sim.Input{Up: !up, Down: up}, view)
		sleep(worldtest.TickInterval)
	}
}

// Two enemies walking at each other with their views a few ticks behind each meet the other where it was drawn, and may stand deep in each other; the world parts them, and never lets them pass through.
func TestAHeadOnClashLeavesEnemiesNoDeeperThanTheParting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := pvpData(t, "1.0", "1.0")
		inst, ayla, robin, as, rs, stop := twoFighters(t, data, nil)
		defer stop()
		before, _ := as.Own(ayla.ID)
		rb, _ := rs.Own(robin.ID)
		for k := range 6 { // some ticks into the clash, on either parity, at every lag
			headOn(inst, as, rs, ayla.ID, robin.ID, k+1, 40)
			a, _ := as.Own(ayla.ID)
			r, _ := rs.Own(robin.ID)
			if d := math.Hypot(a.State.X-r.State.X, a.State.Z-r.State.Z); d < 0.5-0.02 {
				t.Fatalf("after a head-on clash the two stand %.3f m apart; want at least 0.5, no deeper than the parting's 0.25 into each other", d)
			}
			if (a.State.Z-r.State.Z > 0) != (before.State.Z-rb.State.Z > 0) {
				t.Fatalf("Ayla at z %.2f and Robin at %.2f: they passed through each other (they began at %.2f and %.2f)", a.State.Z, r.State.Z, before.State.Z, rb.State.Z)
			}
		}
	})
}

// A skill's dash stops at an enemy's body where it stops at a mob's: Ayla's charge, aimed through Robin, ends with their centres 0.75 m apart.
func TestASkillsDashStopsAtAnEnemy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := pvpData(t, "1.0", "1.0")
		inst, ayla, robin, as, rs, stop := twoFighters(t, data, nil)
		defer stop()
		r, _ := rs.Own(robin.ID)
		a, _ := as.Own(ayla.ID)
		dir := sign(r.State.Z - a.State.Z)
		seq := walkTo(inst, as, ayla.ID, 0, r.State.Z-dir*6.5, 0.2)
		done := make(chan error, 1)
		go func() {
			done <- inst.UseSkill(context.Background(), ayla.ID, ayla.Connection, model.SkillUse{Skill: "charge", Root: "strike", Target: model.EntityRef{Player: robin.ID}, Aim: aimAt(as, ayla.ID, robin.ID), Way: sim.Way{Z: dir, Length: 7}}, seq+1)
		}()
		sleep(worldtest.TickInterval)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		worldtest.Walk(inst, ayla.ID, seq+1, sim.Input{}, 30) // the move starts from the press's next input
		sleep(worldtest.TickInterval)
		if d := apart(as, rs, ayla.ID, robin.ID); d < 0.74 || d > 0.76 {
			t.Fatalf("Ayla's charge through Robin ended %.3f m from his centre; want 0.75", d)
		}
	})
}

// jumpOver has Ayla walk up to Robin, who stands still, then jump toward him and come down just past him; returns the way she went along z.
func jumpOver(t *testing.T, inst *instance.Instance, as, rs *worldtest.Sink, ayla, robin kernel.CharacterID) (dir float64) {
	t.Helper()
	r, _ := rs.Own(robin)
	a, _ := as.Own(ayla)
	dir = sign(r.State.Z - a.State.Z)
	seq := walkTo(inst, as, ayla, 0, r.State.Z-dir*0.75, 0.01)
	for range 4 {
		seq = worldtest.Walk(inst, ayla, seq, sim.Input{Up: dir > 0, Down: dir < 0, Jump: true}, 1)
	}
	seq = worldtest.Walk(inst, ayla, seq, sim.Input{Up: dir > 0, Down: dir < 0}, 8)
	worldtest.Walk(inst, ayla, seq, sim.Input{}, 30) // and stands where she came down, in his body's reach
	return dir
}

// A jump from contact clears an enemy who stands still, as it clears a mob, and the parting leaves the jumper on the far side and the enemy where he stood.
func TestAJumpFromContactClearsAStandingEnemyWithoutAPush(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := pvpData(t, "1.0", "1.0")
		inst, ayla, robin, as, rs, stop := twoFighters(t, data, nil)
		defer stop()
		r0, _ := rs.Own(robin.ID)
		dir := jumpOver(t, inst, as, rs, ayla.ID, robin.ID)
		a, _ := as.Own(ayla.ID)
		r, _ := rs.Own(robin.ID)
		if r.State.Z != r0.State.Z {
			t.Fatalf("Robin stood at z %.4f and the jump moved him to %.4f; want him where he was", r0.State.Z, r.State.Z)
		}
		if past := (a.State.Z - r.State.Z) * dir; past < 0.1 {
			t.Fatalf("Ayla is %.3f m past Robin along her way after the jump; want her over and beyond his centre", past)
		}
	})
}

// A round's reset puts each fighter on their spot even when they swapped sides in the round: the pair's order flips with the placing, and the parting leaves it be.
func TestARoundResetAfterTheFightersSwappedSidesStartsEachOnTheirSpot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := pvpData(t, "1.0", "1.0")
		inst, ayla, robin, as, rs, stop := twoFighters(t, data, nil)
		defer stop()
		jumpOver(t, inst, as, rs, ayla.ID, robin.ID)
		was := len(worldtest.Of[model.Respawned](as))
		for range 4000 {
			if len(worldtest.Of[model.Respawned](as)) > was {
				break
			}
			sleep(worldtest.TickInterval)
		}
		got := worldtest.Of[model.Respawned](as)
		if len(got) <= was {
			t.Fatal("the round never reset")
		}
		sleep(5 * worldtest.TickInterval)
		for _, c := range []struct {
			id  kernel.CharacterID
			s   *worldtest.Sink
			who string
		}{{ayla.ID, as, "Ayla"}, {robin.ID, rs, "Robin"}} {
			var spot model.Respawned
			for _, r := range worldtest.Of[model.Respawned](c.s) {
				if r.ID == c.id {
					spot = r
				}
			}
			if own, _ := c.s.Own(c.id); math.Hypot(own.State.X-spot.X, own.State.Z-spot.Z) > 0.01 {
				t.Fatalf("%s stands at (%.2f, %.2f) after the reset; want her on her spot (%.2f, %.2f)", c.who, own.State.X, own.State.Z, spot.X, spot.Z)
			}
		}
	})
}

// skillPast has Ayla stand reach m short of Robin, who stands still, and use skill on him; returns how far past Robin along her way she ends, and whether Robin moved.
func skillPast(t *testing.T, class kernel.Class, skill, root string, reach float64, way func(dir float64) sim.Way) (past float64, robinMoved bool) {
	t.Helper()
	synctest.Test(t, func(t *testing.T) {
		data := pvpData(t, "1.0", "1.0")
		inst, ayla, robin, as, rs, stop := twoFightersOf(t, data, nil, class)
		defer stop()
		r0, _ := rs.Own(robin.ID)
		a, _ := as.Own(ayla.ID)
		dir := sign(r0.State.Z - a.State.Z)
		seq := walkTo(inst, as, ayla.ID, 0, r0.State.Z-dir*reach, 0.2)
		press := func(skill string, seq uint32) {
			done := make(chan error, 1)
			go func() {
				done <- inst.UseSkill(context.Background(), ayla.ID, ayla.Connection, model.SkillUse{Skill: skill, Root: root, Target: model.EntityRef{Player: robin.ID}, Aim: aimAt(as, ayla.ID, robin.ID), Way: way(dir)}, seq)
			}()
			sleep(worldtest.TickInterval)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		if root != skill { // the root first, then the follow-up it opens
			press(root, seq+1)
			seq = worldtest.Walk(inst, ayla.ID, seq+1, sim.Input{}, 2)
		}
		press(skill, seq+1)
		worldtest.Walk(inst, ayla.ID, seq+1, sim.Input{}, 30)
		sleep(worldtest.TickInterval)
		a, _ = as.Own(ayla.ID)
		r, _ := rs.Own(robin.ID)
		past, robinMoved = (a.State.Z-r.State.Z)*dir, r.State.Z != r0.State.Z
	})
	return past, robinMoved
}

// A skill that puts a fighter past an enemy leaves them there: the pair's order flips, and the parting takes it for the skill's point, not a pass through.
func TestABlinkPastAnEnemyEndsWhereTheSkillPutIt(t *testing.T) {
	past, moved := skillPast(t, "magician", "blink", "blink", 4, func(dir float64) sim.Way { return sim.Way{Z: dir, Length: 6} })
	if past < 1.5 || moved {
		t.Fatalf("after a 6 m blink at an enemy 4 m ahead Ayla is %.2f m past him and he moved: %v; want her 2 m past, he still", past, moved)
	}
}

func TestAShadowStepBehindAnEnemyEndsBehindHim(t *testing.T) {
	past, moved := skillPast(t, "thief", "shadow_step", "double_stab", 4, func(dir float64) sim.Way { return sim.Way{Z: dir, Length: 4.8} })
	if past < 0.5 || moved {
		t.Fatalf("after a shadow step to an enemy 4 m ahead Ayla is %.2f m past him and he moved: %v; want her 0.8 m behind him, he still", past, moved)
	}
}

// A fighter pressed against an enemy stops where the page's sim stops from the wire's numbers: the enemy's body is the centimetre the frames give, not the place off the grid he stands.
func TestAFighterPressedAgainstAnEnemyOffTheCentimetreStopsAtTheWiresBody(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := pvpData(t, "1.0", "1.0")
		inst, ayla, robin, as, rs, stop := twoFighters(t, data, nil)
		defer stop()
		a, _ := as.Own(ayla.ID)
		r0, _ := rs.Own(robin.ID)
		dir := sign(r0.State.Z - a.State.Z)
		worldtest.Walk(inst, robin.ID, 0, sim.Input{Up: dir < 0, Down: dir > 0}, 3) // a few steps off his spot, and a place between two centimetres
		worldtest.Walk(inst, robin.ID, 3, sim.Input{}, 5)
		r, _ := rs.Own(robin.ID)
		if cm := r.State.Z * 100; cm == math.Round(cm) {
			t.Fatalf("Robin stands at z %v, on the centimetre grid; the case wants him off it", r.State.Z)
		}
		seq := walkTo(inst, as, ayla.ID, 0, r.State.Z-dir*4, 0.2)
		seq = worldtest.Walk(inst, ayla.ID, seq, sim.Input{Up: dir > 0, Down: dir < 0}, 200)
		worldtest.Walk(inst, ayla.ID, seq, sim.Input{}, 5)
		a, _ = as.Own(ayla.ID)
		if want := quant.Metres(quant.Position(r.State.Z)) - dir*0.75; math.Abs(a.State.Z-want) > 1e-9 {
			t.Fatalf("Ayla pressed against Robin stops at z %.6f; the page's sim stops her at %.6f from his wire place", a.State.Z, want)
		}
	})
}

// A walk jump pressed anywhere in a wide window short of an enemy clears him, not only a hair's breadth of one: from 1.8 and 2.3 m short, the jumper lands beyond and the enemy stands where he stood (pvp.md section 19: "a jump over an enemy should be easy").
func TestAWalkJumpFromAnywhereInTheWindowClearsAnEnemy(t *testing.T) {
	for _, short := range []float64{1.8, 2.3} {
		synctest.Test(t, func(t *testing.T) {
			data := pvpData(t, "1.0", "1.0")
			inst, ayla, robin, as, rs, stop := twoFighters(t, data, nil)
			defer stop()
			r0, _ := rs.Own(robin.ID)
			a, _ := as.Own(ayla.ID)
			dir := sign(r0.State.Z - a.State.Z)
			seq := walkTo(inst, as, ayla.ID, 0, r0.State.Z-dir*short, 0.1)
			seq = worldtest.Walk(inst, ayla.ID, seq, sim.Input{Up: dir > 0, Down: dir < 0, Jump: true}, 1)
			worldtest.Walk(inst, ayla.ID, seq, sim.Input{Up: dir > 0, Down: dir < 0}, 80)
			a, _ = as.Own(ayla.ID)
			r, _ := rs.Own(robin.ID)
			if r.State.Z != r0.State.Z || (a.State.Z-r.State.Z)*dir < 1 {
				t.Fatalf("a jump %.1f m short: Ayla ends %.2f m past Robin along her way, and he stands at z %.4f (was %.4f); want her well past and him unmoved", short, (a.State.Z-r.State.Z)*dir, r.State.Z, r0.State.Z)
			}
		})
	}
}
