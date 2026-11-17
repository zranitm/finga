package instance_test

import (
	"errors"
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

// statusData is skillData with the slime's hop putting on hop, a status in mobs.yaml's form ("" for none), the slime past any kill unless mortal, and three Warrior skills: Venom, a buff whose hits poison, power 1, for 4 s; Glare, a status skill taunting the mobs within 6 m; and Barrier, a shield of 20 × magic attack for 10 s.
func statusData(t *testing.T, hop string, mortal bool) *gamedata.Data {
	t.Helper()
	edits := map[string]func(string) string{
		"classes.yaml": edit(t, "skills: [slash_blast, iron_body, rally]", "skills: [slash_blast, iron_body, rally, venom, glare, barrier]"),
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
  - id: barrier
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
`
		},
	}
	mobs := func(s string) string { return s }
	if hop != "" {
		mobs = edit(t, "attacks: [{use: hop}]", "attacks: [{use: hop, status: "+hop+"}]") // the green slime's, first in the file
	}
	if !mortal {
		tough := edit(t, "hp: 34", "hp: 1000000")
		inner := mobs
		mobs = func(s string) string { return tough(inner(s)) }
	}
	edits["mobs.yaml"] = mobs
	return skillDataWith(t, edits)
}

// statusCaster is caster by the slime, in its hop's reach and a swing's, with a point in Venom, Glare and Barrier.
func statusCaster() model.Player {
	p := caster(nextToSlime)
	p.Skills.Points["venom"], p.Skills.Points["glare"], p.Skills.Points["barrier"] = 1, 1, 1
	return p
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

// C19: a mob's stun keeps a player from skills and attacks; for 2 s after it ends, the next stun doesn't take.
func TestAStunKeepsAPlayerFromActingThenTheyreImmune(t *testing.T) {
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
		_ = inst.Attack(p.ID, 0, 0)
		tick(2)
		if n := attacksBy(s, p.ID); n != 0 {
			t.Fatalf("%d attacks swung while stunned, want none", n)
		}
		mobHitsOn(t, s, p.ID, 2) // 50 ticks on: 30 into the 40 of immunity the stun's end started
		if err := cast(inst, p.ID, "rally"); err != nil {
			t.Fatalf("a skill after a stun in the immunity: %v, want it used", err)
		}
		mobHitsOn(t, s, p.ID, 3) // 50 more: the immunity over, the stun takes
		if err := cast(inst, p.ID, "rally"); !errors.Is(err, model.ErrCantAct) {
			t.Fatalf("a skill after the immunity's stun: %v, want CANT_ACT", err)
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
		_ = inst.Attack(p.ID, 0, 0)
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
			if d.Amount != want || d.Crit || d.Miss {
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
		if err := cast(inst, p.ID, "barrier"); err != nil {
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
		if v.HP != before.HP || v.Shield != before.Shield-hit.Amount-got[1].Amount-got[2].Amount || got[1].Tick != hit.Tick+21 {
			t.Fatalf("after the hop and its poison %+v: HP %d from %d, shield %d from %d; want the shield to take them all", got, v.HP, before.HP, v.Shield, before.Shield)
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
		_ = inst.Attack(p.ID, 0, 0)
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

// C19: the next hop on a frozen player shatters it for 1.5 times, and starts the immunity; the next on a sleeper wakes them.
func TestAFreezeShattersAndASleepWakes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, statusData(t, "{id: freeze, time: 3s}", false), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		for n := 1; n < 4; n++ {
			mobHitsOn(t, s, p.ID, n) // a hop each 2.5 s, past its 3 s wait at once
		}
		var amounts []int
		for _, d := range mobHitsOn(t, s, p.ID, 4) {
			amounts = append(amounts, d.Amount)
		}
		// Frozen by the first, shattered by the second, which can't freeze in the immunity; the third freezes again, the fourth shatters.
		if want := []int{5, 8, 5, 8}; len(amounts) != 4 || amounts[0] != want[0] || amounts[1] != want[1] || amounts[2] != want[2] || amounts[3] != want[3] {
			t.Fatalf("hops %v, want %v", amounts, want)
		}
	})
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, statusData(t, "{id: sleep, time: 3s}", false), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := statusCaster()
		s := join(t, inst, p)
		mobHitsOn(t, s, p.ID, 1)
		if err := cast(inst, p.ID, "rally"); !errors.Is(err, model.ErrCantAct) {
			t.Fatalf("a skill asleep: %v, want CANT_ACT", err)
		}
		mobHitsOn(t, s, p.ID, 2) // wakes them, in time to act
		if err := cast(inst, p.ID, "rally"); err != nil {
			t.Fatalf("a skill once woken: %v, want it used", err)
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
		_ = inst.Attack(p.ID, 0, 0)
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
		_ = inst.Attack(p.ID, 0, 0)
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
		town := data.StartTown()
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

// C19: statuses stay on the map: a stunned player who takes a portal acts on the next. Until a stun holds a player (statuses' step 3), they walk to it.
func TestAMapChangeLeavesStatusesBehind(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := statusData(t, "{id: stun, time: 5s}", false)
		town := data.StartTown()
		m := arena(t, time.Hour)
		m.Portals = []gamedata.Portal{{Name: "out", At: gamedata.Point{X: arenaX, Z: 15}, Radius: 1.2, TargetMap: town.ID, TargetPortal: town.Portals[0].Name}} // 3.5 m south of the player
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
		seq := uint32(0)
		for range 40 {
			if v, _ := s.Own(p.ID); v.State.Z <= 15.2 {
				break
			}
			seq = worldtest.Walk(inst, p.ID, seq, sim.Input{Down: true}, 1)
		}
		worldtest.Walk(inst, p.ID, seq, sim.Input{}, 10) // standing in it, on the ground
		_ = inst.EnterPortal(p.ID)
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
