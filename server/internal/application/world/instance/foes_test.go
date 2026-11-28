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
		for range 3 { // some ticks into the clash, on either parity
			headOn(inst, as, rs, ayla.ID, robin.ID, 4, 60)
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
