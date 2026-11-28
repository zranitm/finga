package instance_test

import (
	"context"
	"errors"
	"math"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
)

// statusData is skillData with the slime's hop putting on hop, a status in mobs.yaml's form ("" for none), the slime past any kill unless mortal, and four Warrior skills: Venom, a buff whose hits poison, power 1, for 4 s; Glare, a status skill taunting the mobs within 6 m; Barrier, a shield of 20 × magic attack for 10 s; and Shadow Step, an 8 m blink.
func statusData(t *testing.T, hop string, mortal bool) *gamedata.Data {
	attacks := ""
	if hop != "" {
		attacks = "[{use: hop, status: " + hop + "}]"
	}
	return slimeAttacking(t, attacks, mortal)
}

// slimeAttacking is statusData with the slime's attacks the list given, in mobs.yaml's form, taken in turn ("" keeps its plain hop): a second attack lands while the first's hold is on, which the slime's own next hop, a hold being shorter than its cooldown, never does.
func slimeAttacking(t *testing.T, attacks string, mortal bool) *gamedata.Data {
	t.Helper()
	edits := map[string]func(string) string{
		"classes.yaml": edit(t, "bastion, rally]", "bastion, rally, venom, glare, test_barrier, test_shadow_step]"),
		"skills.yaml": func(s string) string {
			return s + `
  - id: venom
    icon: lorc/poison-bottle
    kind: active
    effect: buff
    shape: {kind: area, radius: 0.5}
    energy: 2
    cooldown: 1s
    length: 1050ms
    duration: 10s
    on_hit: {status: poison, time: 4s, power: {base: 1}}
    stages:
      - {at: 0, name: Venom, description: "Hits poison."}
  - id: glare
    icon: lorc/shouting
    kind: active
    effect: status
    shape: {kind: area, radius: 6}
    energy: 2
    cooldown: 1s
    length: 1050ms
    status: {id: taunt, time: 5s}
    stages:
      - {at: 0, name: Glare, description: "Taunts."}
  - id: test_barrier
    icon: lorc/bolt-shield
    kind: active
    effect: buff
    shape: {kind: area, radius: 0.5}
    energy: 2
    cooldown: 1s
    length: 1050ms
    duration: 10s
    shield: {power: {base: 20}}
    stages:
      - {at: 0, name: Barrier, description: "A shield."}
  - id: test_shadow_step
    icon: lorc/ninja-heroic-stance
    kind: active
    effect: damage
    damage: physical
    shape: {kind: arc, radius: 1.875, angle: 90, height: 1.75}
    energy: 2
    cooldown: 1s
    length: 1s
    power: {base: 1}
    move: {kind: blink, distance: 8, toward: lock}
    stages:
      - {at: 0, name: Shadow Step, description: "Steps behind the lock."}
`
		},
	}
	mobs := func(s string) string { return s }
	if attacks != "" {
		mobs = edit(t, "attacks: [{use: hop}]", "attacks: "+attacks) // the green slime's, first in the file
	}
	if !mortal {
		tough := edit(t, "hp: 34", "hp: 1000000")
		inner := mobs
		mobs = func(s string) string { return tough(inner(s)) }
	}
	edits["mobs.yaml"] = mobs
	return skillDataWith(t, edits)
}

// statusCaster is caster by the slime, in its hop's reach and a swing's, with a point in Venom, Glare, Barrier and Shadow Step.
func statusCaster() model.Player {
	p := caster(nextToSlime)
	for _, id := range []string{"venom", "glare", "test_barrier", "test_shadow_step"} {
		p.Skills.Points[id] = 1
	}
	return p
}

// stepped is s after one input of in, three sub-steps, as the world steps it over g.
func stepped(s sim.State, in sim.Input, g sim.Geometry, p sim.Params) sim.State {
	for range 3 {
		s = sim.Step(s, in, g, p)
	}
	return s
}

// C19: a stun holds a player where they are, keys and jump pressed, their heading kept, stepped as the sim steps it; once it ends they walk, their view showing the stun, then none.
func TestAStunHoldsAPlayerThenTheyWalk(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data, m := statusData(t, "{id: stun, time: 1s}", false), arena(t, time.Hour)
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		ends := mobHitsOn(t, s, p.ID, 1)[0].Tick + 21 // from the tick after the hop, for its 20
		if v, _ := s.Own(p.ID); !slices.Equal(v.Statuses, []model.StatusView{{ID: "stun", Ends: ends}}) {
			t.Fatalf("statuses %+v; want the stun to tick %d", v.Statuses, ends)
		}
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{}, 8) // landed from the hop
		landed := viewAfter(t, s, p.ID, seq)
		pressing := sim.Input{Down: true, Right: true, Jump: true}
		seq = worldtest.Walk(inst, p.ID, seq, pressing, 1)
		if got, want := viewAfter(t, s, p.ID, seq), stepped(landed, pressing, m.Geometry, data.Movement()); got != want || got.X != landed.X || got.Z != landed.Z || got.HZ != landed.HZ || got.Held == 0 {
			t.Fatalf("pressing south-east and jump while stunned: %+v from %+v; want %+v, held as it was", got, landed, want)
		}
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 13) // standing to two ticks past the stun's end
		v, _ := s.Own(p.ID)
		if len(v.Statuses) != 0 {
			t.Fatalf("statuses %+v; want none past the stun's end at tick %d", v.Statuses, ends)
		}
		free := v.State
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{Down: true}, 1)
		if got := viewAfter(t, s, p.ID, seq); free.Held != 0 || got.Z >= free.Z {
			t.Fatalf("after the stun %+v then %+v; want it gone and the player walking south", free, got)
		}
	})
}

// C19: a death ends the statuses' hold on the body with them: the frame of the death holds nothing.
func TestADeathEndsTheHold(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := slimeAttacking(t, "[{use: hop, status: {id: stun, time: 2s}}, {use: spit}]", false)
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{Transfer: &transferRecorder{}})
		defer stop()
		tick(1)
		p := statusCaster()
		p.HP = 6 // the hop's 5 stuns them, and the spit after it kills them, stunned
		s := join(t, inst, p)
		mobHitsOn(t, s, p.ID, 1)
		if v := viewAfter(t, s, p.ID, worldtest.Walk(inst, p.ID, 0, sim.Input{}, 2)); v.Held == 0 {
			t.Fatal("not held by the stun")
		}
		untilDead(t, s, p.ID)
		if v, _ := s.Own(p.ID); !v.Dead || v.State.Held != 0 || v.State.Slow != (sim.Slow{}) || len(v.Statuses) != 0 {
			t.Fatalf("dead %+v; want no statuses and no hold", v)
		}
	})
}

// C19: a slow walks a player at 0.6 of their speed, stepped as the sim steps it, then at full speed once it ends.
func TestASlowSlowsAPlayersWalk(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data, m := statusData(t, "{id: slow, time: 1s}", false), arena(t, time.Hour)
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		mobHitsOn(t, s, p.ID, 1)
		warrior, _ := data.Class("warrior")
		derived := instance.StatsOf(data, warrior, 1, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived
		params := data.Movement().Faster(sim.SpeedFactor(derived.MoveSpeed)).Fighting() // the player's own pace, hit so in combat, as the world steps them
		walk := func(seq uint32) (sim.State, sim.State, uint32) {
			v, _ := s.Own(p.ID)
			seq = worldtest.Walk(inst, p.ID, seq, sim.Input{Down: true}, 1)
			return v.State, viewAfter(t, s, p.ID, seq), seq
		}
		before, got, seq := walk(worldtest.Walk(inst, p.ID, 0, sim.Input{}, 8))
		if want := stepped(before, sim.Input{Down: true}, m.Geometry, params); got != want || got.Slow.Factor != 0.6 || math.Abs(before.Z-got.Z-0.6*params.WalkSpeed*3*params.Dt) > 1e-9 {
			t.Fatalf("slowed, a step south from %+v: %+v; want %+v, at 0.6 of the walk", before, got, want)
		}
		before, got, _ = walk(worldtest.Walk(inst, p.ID, seq, sim.Input{}, 12)) // past the slow's end
		if before.Slow != (sim.Slow{}) || math.Abs(before.Z-got.Z-params.WalkSpeed*3*params.Dt) > 1e-9 {
			t.Fatalf("once the slow ends, a step south from %+v: %+v; want the full walk", before, got)
		}
	})
}

// A mob's blow and the status it inflicts reach the client in one Delta: the hit's own carries the hold or slow the next input steps with, so the client's one correction for the knockback covers them, not a second a tick later.
func TestAHitsStatusIsInTheHitsOwnDelta(t *testing.T) {
	for _, tt := range []struct {
		status string
		want   func(sim.State) bool
	}{
		{"{id: slow, time: 1s}", func(s sim.State) bool { return s.Slow == sim.Slow{Left: 20 * 3, Factor: 0.6} && s.Held == 0 }},
		{"{id: pin, time: 2s}", func(s sim.State) bool { return s.Held == 40*3 && s.Slow == sim.Slow{} }},
	} {
		t.Run(tt.status, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, statusData(t, tt.status, false), arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := statusCaster()
				s := join(t, inst, p)
				hit := mobHitsOn(t, s, p.ID, 1)[0]
				var got *sim.State
				for _, d := range s.Deltas() {
					for _, v := range d.Players {
						if d.Tick == hit.Tick && v.ID == p.ID {
							got = &v.State
						}
					}
				}
				if got == nil || !tt.want(*got) {
					t.Fatalf("the hit's Delta at tick %d: %+v; want it holding or slowing for the status's ticks from the next, 3 sub-steps each", hit.Tick, got)
				}
			})
		})
	}
}

// C19: a pinned player still uses a moving skill, but its move doesn't start, not even a blink's, which the sim's hold can't stop: they stay where they stand.
func TestAPinnedPlayersBlinkStaysPut(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, statusData(t, "{id: pin, time: 2s}", false), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		mobHitsOn(t, s, p.ID, 1)
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{}, 8) // landed from the hop
		pinned := viewAfter(t, s, p.ID, seq)
		done := useWay(inst, p.ID, "test_shadow_step", sim.Way{Z: -1, Length: 8}, seq)
		tick(1)
		if err := <-done; err != nil {
			t.Fatalf("a blink while pinned: %v, want it used", err)
		}
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 3)
		if got := viewAfter(t, s, p.ID, seq); got.Move != (sim.Move{}) || got.X != pinned.X || got.Z != pinned.Z || got.Held == 0 {
			t.Fatalf("after a blink pressed while pinned: %+v from %+v; want no move, the player where they were", got, pinned)
		}
	})
}

// attacksBy are the basic attacks p swung that s heard.
func attacksBy(s *worldtest.Sink, id kernel.CharacterID) int {
	n := 0
	for _, a := range eventsOf[model.AttackUsed](s) {
		if a.Attacker == id {
			n++
		}
	}
	return n
}

// C19: a mob's stun keeps a player from skills and attacks; once it ends they act, and the next stun takes at once.
func TestAStunKeepsAPlayerFromActingAndTheNextTakes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, statusData(t, "{id: stun, time: 1s}", false), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		mobHitsOn(t, s, p.ID, 1) // stunned from the next tick for 20
		if err := cast(inst, p.ID, "rally"); !errors.Is(err, model.ErrCantAct) {
			t.Fatalf("a skill while stunned: %v, want CANT_ACT", err)
		}
		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		tick(2)
		if n := attacksBy(s, p.ID); n != 0 {
			t.Fatalf("%d attacks swung while stunned, want none", n)
		}
		tick(25) // the stun's 20 ticks over
		if err := cast(inst, p.ID, "rally"); err != nil {
			t.Fatalf("a skill once the stun ended: %v, want it used", err)
		}
		mobHitsOn(t, s, p.ID, 2) // 50 ticks on from the first, 30 past its end
		if err := cast(inst, p.ID, "rally"); !errors.Is(err, model.ErrCantAct) {
			t.Fatalf("a skill under the next stun: %v, want CANT_ACT", err)
		}
	})
}

// C19: silence refuses skills, and basic attacks go on.
func TestASilencedPlayerStillAttacks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, statusData(t, "{id: silence, time: 2s}", false), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		mobHitsOn(t, s, p.ID, 1)
		if err := cast(inst, p.ID, "strike"); !errors.Is(err, model.ErrSilenced) {
			t.Fatalf("a skill while silenced: %v, want SILENCED", err)
		}
		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		tick(2)
		if n := attacksBy(s, p.ID); n != 1 {
			t.Fatalf("%d attacks swung while silenced, want 1", n)
		}
	})
}

// C19: a mob's poison ticks on a player each second, its steady damage from the mob, and can kill; the dead take no more.
func TestAMobsPoisonTicksOnAPlayer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := statusData(t, "{id: poison, time: 4s, power: 0.5}", false)
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{Transfer: &transferRecorder{}})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		hit := mobHitsOn(t, s, p.ID, 1)[0].Tick
		tick(48)
		warrior, _ := data.Class("warrior")
		slime, _ := data.Mob("green_slime")
		derived := instance.StatsOf(data, warrior, 1, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived
		want := model.Steady(model.MobAttacker(slime, 0.5), model.PlayerDefender(derived))
		var ticks []uint64
		for _, d := range mobHitsOn(t, s, p.ID, 1)[1:] {
			if d.Amount != want || d.Crit || d.Miss || d.Status != "poison" {
				t.Fatalf("poison %+v, want %d", d, want)
			}
			ticks = append(ticks, d.Tick)
		}
		if len(ticks) != 2 || ticks[0] != hit+21 || ticks[1] != hit+41 {
			t.Fatalf("poison at ticks %v; want %d and %d, a second apart from the tick after the hop", ticks, hit+21, hit+41)
		}

		dying := statusCaster()
		dying.HP = 6 // the hop's 5 leaves 1, the poison's tick kills
		s2 := join(t, inst, dying)
		untilDead(t, s2, dying.ID)
		died := worldtest.Of[model.Died](s2)[0].Tick
		tick(30)
		for _, d := range damages(s2) {
			if d.Target.Player == dying.ID && d.Tick > died {
				t.Fatalf("damage %+v on the dead", d)
			}
		}
	})
}

// C19: a shield takes a mob's poison before HP, as it takes its blows; the hit guard after a blow doesn't stop a tick.
func TestAShieldTakesPoisonFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, statusData(t, "{id: poison, time: 4s, power: 0.5}", false), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		if err := cast(inst, p.ID, "test_barrier"); err != nil {
			t.Fatal(err)
		}
		before, _ := s.Own(p.ID)
		if before.Shield == 0 {
			t.Fatal("no shield")
		}
		hit := mobHitsOn(t, s, p.ID, 1)[0]
		tick(42) // the poison's ticks at 21 and 41
		got := mobHitsOn(t, s, p.ID, 3)
		v, _ := s.Own(p.ID)
		if v.HP != before.HP || v.Shield != before.Shield-hit.Amount-got[1].Amount-got[2].Amount || got[1].Tick != hit.Tick+21 || got[1].Shielded != got[1].Amount || got[2].Shielded != got[2].Amount {
			t.Fatalf("after the hop and its poison %+v: HP %d from %d, shield %d from %d; want the shield to take them all", got, v.HP, before.HP, v.Shield, before.Shield)
		}
	})
}

// C19: poison past a shield the hop broke takes it all off HP, and its Damage says the shield took none.
func TestPoisonPastABrokenShieldTakesHP(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, statusData(t, "{id: poison, time: 4s, power: 0.5}", false), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		p.Timers = model.SkillTimers{Buffs: map[string]model.BuffTimer{"test_barrier": {Until: time.Now().Add(time.Hour), Points: 1, Shield: 1}}}
		s := join(t, inst, p)
		hop := mobHitsOn(t, s, p.ID, 1)[0]
		after, _ := s.Own(p.ID)
		tick(22) // the poison's tick at 21
		got := mobHitsOn(t, s, p.ID, 2)
		v, _ := s.Own(p.ID)
		if hop.Shielded != 1 || got[1].Status != "poison" || got[1].Shielded != 0 || v.HP != after.HP-got[1].Amount {
			t.Fatalf("the hop %+v then poison %+v: HP %d from %d; want the hop to break the shield of 1 and the poison all off HP", hop, got[1], v.HP, after.HP)
		}
	})
}

// C19: the hit guard after a blow stops blows, not damage over time: a burn every half second ticks the tick after the next hop.
func TestTheHitGuardDoesntStopDamageOverTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, statusData(t, "{id: burn, time: 3s, power: 0.5}", false), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		first := mobHitsOn(t, s, p.ID, 1)[0].Tick
		for n := 2; n <= 7; n++ { // the burn's five ticks to the next hop, which restarts it on its beat
			mobHitsOn(t, s, p.ID, n)
		}
		tick(2)
		guarded := false
		for _, d := range mobHitsOn(t, s, p.ID, 8) {
			guarded = guarded || d.Tick == first+51 // the hop at first+50 guards to first+60
		}
		if !guarded {
			t.Fatalf("no burn at tick %d, inside the next hop's guard; mob damage %+v", first+51, mobHitsOn(t, s, p.ID, 1))
		}
	})
}

// C19: armour break lets a mob's hops through harder; weaken takes a quarter off the player's own blows.
func TestArmourBreakAndWeakenOnAPlayer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, statusData(t, "{id: armor_break, time: 30s}", false), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		// Slime attack 6 against defense 12, 5; against 8.4, 6·100/108.4 = 5.5 → 6.
		mobHitsOn(t, s, p.ID, 1)
		if hits := mobHitsOn(t, s, p.ID, 2); hits[0].Amount != 5 || hits[1].Amount != 6 {
			t.Fatalf("hops %d then %d; want 5, then 6 through the broken armour", hits[0].Amount, hits[1].Amount)
		}
	})
	synctest.Test(t, func(t *testing.T) {
		data := statusData(t, "{id: weaken, time: 30s}", false)
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		mobHitsOn(t, s, p.ID, 1)
		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		tick(20)
		warrior, _ := data.Class("warrior")
		slime, _ := data.Mob("green_slime")
		attack := model.PlayerAttacker(instance.StatsOf(data, warrior, 1, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived, gamedata.Physical)
		attack.PhysicalAttack, attack.MagicAttack = attack.PhysicalAttack*0.75, attack.MagicAttack*0.75
		want := model.Resolve(attack, model.MobDefender(slime), data.StatRules().Hit, worldtest.FixedRoll{V: 0.5})
		var got []model.Damage
		for _, d := range damages(s) {
			if d.Source.Player == p.ID {
				got = append(got, d)
			}
		}
		if len(got) != 1 || got[0].Amount != want.Damage {
			t.Fatalf("blows %+v; want one of %d, a quarter off", got, want.Damage)
		}
	})
}

// C19: the next hop on a frozen player shatters it for 1.5 times, then freezes them again; the next on a sleeper wakes them.
func TestAFreezeShattersAndASleepWakes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := slimeAttacking(t, "[{use: hop, status: {id: freeze, time: 2s}}, {use: spit, status: {id: freeze, time: 2s}}]", false)
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		for n := 1; n < 4; n++ {
			mobHitsOn(t, s, p.ID, n) // a hop, then a spit, in turn, each landing within the 2 s freeze the one before put on
		}
		var amounts []int
		for _, d := range mobHitsOn(t, s, p.ID, 4) {
			amounts = append(amounts, d.Amount)
		}
		// Frozen by the first; each after shatters the freeze the one before put on, and freezes again.
		if want := []int{5, 8, 8, 8}; len(amounts) != 4 || amounts[0] != want[0] || amounts[1] != want[1] || amounts[2] != want[2] || amounts[3] != want[3] {
			t.Fatalf("hops %v, want %v", amounts, want)
		}
	})
	synctest.Test(t, func(t *testing.T) { // the slime's hop puts them to sleep, and a boar's plain bite wakes them before the next hop
		m := worldtest.Gridded(t, &gamedata.Map{
			ID: "arena", Kind: gamedata.Region, Geometry: worldtest.Flat(40),
			Spawns: []gamedata.Spawn{
				{Mob: "green_slime", Max: 1, Respawn: time.Hour, At: gamedata.Point{X: arenaX, Z: slimeZ}, Radius: 0.01},
				{Mob: "wild_boar", Max: 1, Respawn: time.Hour, At: gamedata.Point{X: arenaX, Z: nextToSlime - 1.5}, Radius: 0.01},
			},
			PlayerSpawn: gamedata.Point{X: arenaX, Z: spawnZ},
		})
		inst, stop := worldtest.RunMap(t, statusData(t, "{id: sleep, time: 2s}", false), m, nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		var slime uint64
		for _, mv := range worldtest.Of[model.Snapshot](s)[0].Mobs {
			if mv.Type == "green_slime" {
				slime = mv.ID
			}
		}
		var asleep, woken uint64
		for range 120 {
			for _, d := range damages(s) {
				switch {
				case d.Target.Player != p.ID || !d.Source.IsMob():
				case d.Source.Mob == slime && asleep == 0:
					asleep = d.Tick
				case d.Source.Mob == slime && d.Tick > asleep && woken == 0:
					t.Fatalf("the slime hopped again at tick %d before the boar bit", d.Tick)
				case d.Source.Mob != slime && asleep > 0 && d.Tick > asleep && woken == 0:
					woken = d.Tick
				}
			}
			if asleep > 0 && woken == 0 {
				if err := cast(inst, p.ID, "rally"); !errors.Is(err, model.ErrCantAct) {
					t.Fatalf("a skill asleep: %v, want CANT_ACT", err)
				}
			}
			if woken > 0 {
				break
			}
			tick(1)
		}
		if woken == 0 {
			t.Fatal("the boar never bit the sleeper")
		}
		if err := cast(inst, p.ID, "rally"); err != nil {
			t.Fatalf("a skill once the boar's bite woke them at tick %d: %v, want it used", woken, err)
		}
	})
}

// C19: a buff's on_hit poisons what its user's blows land on; the poison's damage is theirs, and so is its kill and EXP.
func TestAPlayersPoisonIsTheirs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := statusData(t, "", true)
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		if err := cast(inst, p.ID, "venom"); err != nil {
			t.Fatal(err)
		}
		tick(21) // Venom's action
		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		tick(45)
		var mine []model.Damage
		for _, d := range damages(s) {
			if d.Source.Player == p.ID {
				mine = append(mine, d)
			}
		}
		if len(mine) != 2 || mine[1].Tick != mine[0].Tick+21 || mine[1].Crit || mine[1].Miss {
			t.Fatalf("blows %+v; want the swing, then its poison's first tick a second after the next", mine)
		}
		warrior, _ := data.Class("warrior")
		slime, _ := data.Mob("green_slime")
		attack := model.PlayerAttacker(instance.StatsOf(data, warrior, 1, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived, gamedata.Physical)
		if want := model.Steady(attack, model.MobDefender(slime)); mine[1].Amount != want {
			t.Fatalf("the poison's tick %d, want %d: the attack at the swing, steady", mine[1].Amount, want)
		}
		if exp := eventsOf[model.ExpGained](s); len(exp) != 1 || exp[0].Amount != slime.Exp {
			t.Fatalf("exp %+v; want the slime's %d for the poison's kill", exp, slime.Exp)
		}
	})
}

// C19: a player's poison on a mob ends when they leave the map.
func TestLeavingEndsAPlayersPoison(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, statusData(t, "", false), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		join(t, inst, p)
		watcher := join(t, inst, warriorAt(outOfSight))
		if err := cast(inst, p.ID, "venom"); err != nil {
			t.Fatal(err)
		}
		tick(21)
		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		tick(10) // the swing has landed, its poison not yet ticked
		theirs := func() int {
			n := 0
			for _, d := range damages(watcher) {
				if d.Source.Player == p.ID {
					n++
				}
			}
			return n
		}
		if n := theirs(); n != 1 {
			t.Fatalf("%d blows seen; want the swing", n)
		}
		hp := func() int {
			for _, m := range mobsIn(watcher) {
				return m.HP
			}
			t.Fatal("no slime seen")
			return 0
		}
		struck := hp()
		if err := inst.Leave(p.ID, "c1", nil); err != nil {
			t.Fatal(err)
		}
		tick(60)
		if n, now := theirs(), hp(); n != 1 || now != struck {
			t.Fatalf("%d blows seen, the slime at %d HP from %d; want its poison gone with them", n, now, struck)
		}
	})
}

// C19: a status skill lands no damage, and like a damaging one, a town refuses it.
func TestAStatusSkillHurtsNothingAndATownRefusesIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := statusData(t, "", false)
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(farFromSlime)
		p.Skills.Points["glare"] = 1
		s := join(t, inst, p)
		if err := cast(inst, p.ID, "glare"); err != nil {
			t.Fatal(err)
		}
		tick(25)
		for _, d := range damages(s) {
			if d.Source.Player == p.ID {
				t.Fatalf("damage %+v from a status skill", d)
			}
		}
	})
	synctest.Test(t, func(t *testing.T) {
		data := statusData(t, "", false)
		town := worldtest.Town(data)
		inst, stop := worldtest.RunMap(t, data, town, nil, worldtest.Options{})
		defer stop()
		p := statusCaster()
		p.X, p.Z = town.PlayerSpawn.X, town.PlayerSpawn.Z
		join(t, inst, p)
		if err := cast(inst, p.ID, "glare"); !errors.Is(err, model.ErrNotInTown) {
			t.Fatalf("a status skill in a town: %v, want NOT_IN_TOWN", err)
		}
	})
}

// C19: statuses stay on the map: a stunned player who takes a portal acts on the next.
func TestAMapChangeLeavesStatusesBehind(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := statusData(t, "{id: stun, time: 2s}", false)
		town := worldtest.Town(data)
		m := arena(t, time.Hour)
		m.Portals = []gamedata.Portal{{Name: "out", At: gamedata.Point{X: arenaX, Z: nextToSlime - 0.8}, Radius: 1, TargetMap: town.ID, TargetPortal: town.Portals[0].Name}} // round where the hop leaves them, clear of the slime's reach
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{Transfer: tr})
		defer stop()
		next, stopNext := worldtest.RunMap(t, data, town, nil, worldtest.Options{})
		defer stopNext()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		mobHitsOn(t, s, p.ID, 1)
		if err := cast(inst, p.ID, "rally"); !errors.Is(err, model.ErrCantAct) {
			t.Fatalf("a skill while stunned: %v, want CANT_ACT", err)
		}
		worldtest.Walk(inst, p.ID, 0, sim.Input{}, 10) // landed from the hop, in the portal
		if err := inst.EnterPortal(context.Background(), p.ID, 0); err != nil {
			t.Fatal(err)
		}
		tick(1)
		if moved(tr) != 1 {
			t.Fatal("the portal moved nobody")
		}
		join(t, next, tr.moved[0])
		if err := cast(next, p.ID, "rally"); err != nil {
			t.Fatalf("a skill on the next map: %v, want it used, the stun left behind", err)
		}
	})
}
