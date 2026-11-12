package instance_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/pkg/transient"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// skillData is the shipped data with a heal the Beginner teaches and a buff the Warrior teaches: Mend heals the caster and the nearest party member within 120 px by magic attack × 2, for 5 MP; Rally adds 50 physical attack for 2 s, for 3 MP.
func skillData(t *testing.T) *gamedata.Data {
	t.Helper()
	return worldtest.LoadDataWith(t, map[string]func(string) string{
		"classes.yaml": func(s string) string {
			s = strings.Replace(s, "skills: [strike, throw, spark, second_wind]", "skills: [strike, throw, spark, second_wind, mend]", 1)
			return strings.Replace(s, "skills: [slash_blast, iron_body]", "skills: [slash_blast, iron_body, rally]", 1)
		},
		"skills.yaml": func(s string) string {
			return s + `
  - id: mend
    kind: active
    effect: heal
    shape: {kind: area, radius: 120}
    mp: {base: 5}
    cooldown: 2s
    length: 1050ms
    power: {base: 2}
    targets: 2
    stages:
      - {at: 0, name: Mend, description: "Heals the caster and the nearest party member."}
  - id: rally
    kind: active
    effect: buff
    shape: {kind: area, radius: 1}
    mp: {base: 3}
    cooldown: 1s
    length: 1050ms
    duration: 2s
    bonus: {physical_attack: {base: 50}}
    stages:
      - {at: 0, name: Rally, description: "Hits harder for a while."}
`
		},
	})
}

// caster is a level 1 warrior at x with the starter sword, joined through c1, with a point in each skill.
func caster(x float64) model.Player {
	p := warriorAt(x)
	p.Connection = "c1"
	p.Skills = model.Skills{Points: map[string]int{"strike": 1, "second_wind": 1, "mend": 1, "rally": 1}}
	return p
}

func castAsync(inst *instance.Instance, id kernel.CharacterID, skill string) <-chan error {
	done := make(chan error, 1)
	go func() { done <- inst.UseSkill(context.Background(), id, "c1", skill) }()
	return done
}

// cast uses the skill on the next tick and returns its answer.
func cast(inst *instance.Instance, id kernel.CharacterID, skill string) error {
	done := castAsync(inst, id, skill)
	tick(1)
	return <-done
}

func TestASkillIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		town   bool
		edit   func(*model.Player)
		before string // a skill used first
		skill  string
		want   error
	}{
		{name: "with no points in it", skill: "throw", want: model.ErrSkillNotLearned},
		{name: "when the data doesn't have it", edit: func(p *model.Player) { p.Skills.Points["fireball"] = 3 }, skill: "fireball", want: model.ErrSkillNotLearned},
		{name: "when the class can't learn it", edit: func(p *model.Player) { p.Class = "beginner" }, skill: "rally", want: model.ErrSkillNotLearned},
		{name: "when it's passive", skill: "second_wind", want: model.ErrSkillPassive},
		{name: "when it's passive, in a town", town: true, skill: "second_wind", want: model.ErrSkillPassive},
		{name: "when it damages, in a town", town: true, skill: "strike", want: model.ErrNotInTown},
		{name: "during its cooldown", before: "strike", skill: "strike", want: model.ErrSkillCoolingDown},
		{name: "short of MP", edit: func(p *model.Player) { p.MP = 3 }, skill: "strike", want: model.ErrNotEnoughMP},
		{name: "during its cooldown and short of MP", edit: func(p *model.Player) { p.MP = 4 }, before: "strike", skill: "strike", want: model.ErrSkillCoolingDown},
		{name: "a heal with no one hurt", edit: func(p *model.Player) { p.HP = 1_000_000 }, skill: "mend", want: model.ErrHPFull},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := skillData(t)
				m := arena(time.Hour)
				if tc.town {
					m = data.StartTown()
				}
				inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{})
				defer stop()
				p := caster(100)
				if tc.town {
					p.X, p.Y = m.PlayerSpawn.X, m.PlayerSpawn.Y
				}
				if tc.edit != nil {
					tc.edit(&p)
				}
				s := join(t, inst, p)
				if tc.before != "" {
					if err := cast(inst, p.ID, tc.before); err != nil {
						t.Fatal(err)
					}
				}
				used := len(eventsOf[model.SkillUsed](s))
				if err := cast(inst, p.ID, tc.skill); !errors.Is(err, tc.want) {
					t.Fatalf("err = %v, want %v", err, tc.want)
				}
				if len(eventsOf[model.SkillUsed](s)) != used {
					t.Fatal("a refused skill was used")
				}
			})
		})
	}
	t.Run("when dead", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			inst, stop := worldtest.RunMap(t, skillData(t), arena(time.Hour), nil, worldtest.Options{Transfer: &transferRecorder{}})
			defer stop()
			tick(1)
			p := caster(260)
			p.HP = 1
			join(t, inst, p)
			worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 2) // onto the slime, which kills
			if err := cast(inst, p.ID, "mend"); !errors.Is(err, model.ErrCharacterDead) {
				t.Fatalf("err = %v, want ErrCharacterDead", err)
			}
		})
	})
	t.Run("through another connection", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			inst, stop := worldtest.RunMap(t, skillData(t), arena(time.Hour), nil, worldtest.Options{})
			defer stop()
			p := caster(100)
			join(t, inst, p)
			done := make(chan error, 1)
			go func() { done <- inst.UseSkill(context.Background(), p.ID, "c2", "strike") }()
			tick(1)
			if err := <-done; !errors.Is(err, transient.ErrUnavailable) {
				t.Fatalf("err = %v, want ErrUnavailable", err)
			}
		})
	})
}

// crowd is arena with four still slimes on the platform.
func crowd() *gamedata.Map {
	m := arena(time.Hour)
	m.Spawns[0].Max = 4
	return m
}

// Strike hits the nearest mob in its box with physical attack × its power; evolved into Crushing Blow at 25 points, it hits three. The map sees it used.
func TestStrikeHitsWithItsPowerUpToItsTargets(t *testing.T) {
	for _, tc := range []struct {
		points, hits int
		mp           int // left of 28
	}{
		{1, 1, 24},  // 4 + 0.2 MP, rounded
		{25, 3, 19}, // 4 + 5 MP
	} {
		t.Run(fmt.Sprintf("at %d points", tc.points), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := skillData(t)
				inst, stop := worldtest.RunMap(t, data, crowd(), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := caster(250)
				p.Skills.Points["strike"] = tc.points
				s := join(t, inst, p)
				watcher := join(t, inst, warriorAt(150))
				if err := cast(inst, p.ID, "strike"); err != nil {
					t.Fatal(err)
				}
				got := damages(s)
				if len(got) != tc.hits {
					t.Fatalf("%d hits, want %d", len(got), tc.hits)
				}
				hit := map[uint64]bool{}
				for _, d := range got {
					hit[d.Target.Mob] = true
					if d.Source.Player != p.ID || d.Miss || d.Amount <= 24 { // the basic swing's 24, times a power over 1
						t.Fatalf("damage %+v", d)
					}
				}
				if len(hit) != tc.hits {
					t.Fatalf("hit %v; want %d mobs once each", hit, tc.hits)
				}
				stage := map[int]int{1: 0, 25: 2}[tc.points]
				for _, sk := range [][]model.SkillUsed{eventsOf[model.SkillUsed](s), eventsOf[model.SkillUsed](watcher)} {
					if len(sk) != 1 || sk[0].Caster != p.ID || sk[0].Skill != "strike" || sk[0].Stage != stage || sk[0].Facing != 1 {
						t.Fatalf("skills used %+v", sk)
					}
				}
				if changed := eventsOf[model.StatsChanged](s); len(changed) == 0 || changed[0].MP != tc.mp || changed[0].Sheet != nil {
					t.Fatalf("stats changed %+v, want MP %d and no sheet", changed, tc.mp)
				}
			})
		})
	}
}

// Strike's damage is the basic swing's with the attack multiplied by its power.
func TestStrikesPowerMultipliesTheAttack(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := skillData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(250)
		s := join(t, inst, p)
		if err := cast(inst, p.ID, "strike"); err != nil {
			t.Fatal(err)
		}
		warrior, _ := data.Class("warrior")
		slime, _ := data.Mob("green_slime")
		attack := model.PlayerAttacker(instance.StatsOf(data, warrior, 1, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived, gamedata.Physical).Times(1.55)
		want := model.Resolve(attack, model.MobDefender(slime), data.StatRules().Hit, worldtest.FixedRoll{V: 0.5})
		if got := damages(s); len(got) != 1 || got[0].Amount != want.Damage {
			t.Fatalf("damages %+v, want %d: physical attack × 1.55", got, want.Damage)
		}
	})
}

// Mend heals the caster and the nearest hurt party member in reach, by magic attack × 2; a member farther than the nearest, and a stranger, are not healed.
func TestAHealMendsTheCasterAndTheNearestPartyMember(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := skillData(t)
		parties := &fakeParties{}
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{Parties: parties})
		defer stop()
		a, near, far, stranger := caster(600), warriorAt(650), warriorAt(690), warriorAt(620)
		parties.parties = [][]kernel.CharacterID{{a.ID, near.ID, far.ID}}
		sinks := map[kernel.CharacterID]*worldtest.Sink{}
		for _, p := range []*model.Player{&a, &near, &far, &stranger} {
			p.HP = 10
			sinks[p.ID] = join(t, inst, *p)
		}
		if err := cast(inst, a.ID, "mend"); err != nil {
			t.Fatal(err)
		}
		warrior, _ := data.Class("warrior")
		heal := int(math.Round(instance.StatsOf(data, warrior, 1, a.Points, a.Skills, a.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived.MagicAttack * 2))
		for _, c := range []struct {
			name string
			id   kernel.CharacterID
			hp   int
		}{{"the caster", a.ID, 10 + heal}, {"the nearest member", near.ID, 10 + heal}, {"a farther member", far.ID, 10}, {"a stranger", stranger.ID, 10}} {
			if v, _ := sinks[c.id].Own(c.id); v.HP != c.hp {
				t.Errorf("%s has %d HP, want %d", c.name, v.HP, c.hp)
			}
		}
		if changed := eventsOf[model.StatsChanged](sinks[a.ID]); len(changed) != 1 || changed[0].MP != 23 || changed[0].HP != 10+heal {
			t.Fatalf("stats changed %+v, want 5 MP spent", changed)
		}
	})
}

// Only the hurt take a heal's places: a member at full HP nearer than the hurt ones takes none, and a caster at full HP leaves theirs to another.
func TestAHealGoesToTheHurtOnly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := skillData(t)
		parties := &fakeParties{}
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{Parties: parties})
		defer stop()
		a, full, near, far := caster(600), warriorAt(640), warriorAt(660), warriorAt(690)
		parties.parties = [][]kernel.CharacterID{{a.ID, full.ID, near.ID, far.ID}}
		a.HP, full.HP, near.HP, far.HP = 1_000_000, 1_000_000, 10, 10
		sinks := map[kernel.CharacterID]*worldtest.Sink{}
		for _, p := range []*model.Player{&a, &full, &near, &far} {
			sinks[p.ID] = join(t, inst, *p)
		}
		before, _ := sinks[full.ID].Own(full.ID)
		if err := cast(inst, a.ID, "mend"); err != nil {
			t.Fatal(err)
		}
		warrior, _ := data.Class("warrior")
		heal := int(math.Round(instance.StatsOf(data, warrior, 1, a.Points, a.Skills, a.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived.MagicAttack * 2))
		for _, c := range []struct {
			name string
			id   kernel.CharacterID
			hp   int
		}{{"the member at full HP", full.ID, before.HP}, {"the nearer hurt member", near.ID, 10 + heal}, {"the farther hurt member", far.ID, 10 + heal}} {
			if v, _ := sinks[c.id].Own(c.id); v.HP != c.hp {
				t.Errorf("%s has %d HP, want %d", c.name, v.HP, c.hp)
			}
		}
	})
}

// sheets are the StatsChanged that carry a sheet; regen's carry none.
func sheets(s *worldtest.Sink) []model.StatsChanged {
	var out []model.StatsChanged
	for _, c := range eventsOf[model.StatsChanged](s) {
		if c.Sheet != nil {
			out = append(out, c)
		}
	}
	return out
}

// Rally adds its bonus to the sheet as the skills' share, and the attack hits harder, until it ends 2 s later.
func TestABuffAddsToTheStatsUntilItEnds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := skillData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(250)
		s := join(t, inst, p)
		if err := cast(inst, p.ID, "rally"); err != nil {
			t.Fatal(err)
		}
		changed := sheets(s)
		if len(changed) != 1 || changed[0].Sheet.DerivedSkills.PhysicalAttack != 50 || changed[0].MP != 25 {
			t.Fatalf("stats changed %+v, want Rally's 50 in the skills' share and 3 MP spent", changed)
		}
		_ = inst.Attack(p.ID) // it waits for the turn Rally took
		tick(15)
		if got := damages(s); len(got) != 1 || got[0].Amount <= 24 {
			t.Fatalf("damages %+v, want harder than the basic swing's 24", got)
		}
		tick(40)
		if changed = sheets(s); len(changed) != 2 || changed[1].Sheet.DerivedSkills.PhysicalAttack != 0 {
			t.Fatalf("stats changed %+v, want the sheet without Rally once it ends", changed)
		}
	})
}

func TestBuffsEndAtDeath(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, skillData(t), arena(time.Hour), nil, worldtest.Options{Transfer: &transferRecorder{}})
		defer stop()
		tick(1)
		p := caster(260)
		p.HP = 1
		s := join(t, inst, p)
		if err := cast(inst, p.ID, "rally"); err != nil {
			t.Fatal(err)
		}
		worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 2) // onto the slime, which kills
		if changed := sheets(s); len(changed) != 2 || changed[1].Sheet.DerivedSkills.PhysicalAttack != 0 || changed[1].HP != 0 {
			t.Fatalf("stats changed %+v, want the sheet without Rally at death", changed)
		}
	})
}

// A skill takes the next basic attack's turn: sent with an attack, it goes first and the attack a turn later; used during the attack's cooldown, it waits for its end, up to castSlack early, and the next turn counts from that end, so going early gains nothing.
func TestASkillTakesTheNextAttacksTurn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := skillData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(250)
		p.HP, p.MP = 1_000_000, 1000 // full HP, so a heal is refused
		s := join(t, inst, p)
		warrior, _ := data.Class("warrior")
		turn := model.AttackLengthTicks(warrior.Attack.Length, instance.StatsOf(data, warrior, 1, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived.AttackSpeed, worldtest.TickInterval)
		const slack = 3 // castSlack's 150 ms in 50 ms ticks

		done := castAsync(inst, p.ID, "rally")
		_ = inst.Attack(p.ID)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		tick(int(turn))
		used, hits := eventsOf[model.SkillUsed](s), damages(s)
		if len(used) != 1 || len(hits) != 1 || hits[0].Tick != used[0].Tick+turn {
			t.Fatalf("Rally at %+v, the attack at %+v; want the attack a turn of %d ticks after", used, hits, turn)
		}

		done = castAsync(inst, p.ID, "strike")
		tick(1)
		select {
		case err := <-done:
			t.Fatalf("Strike answered %v inside the attack's cooldown", err)
		default:
		}
		tick(int(turn))
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		done = castAsync(inst, p.ID, "mend") // the caster is hurt by nothing, so Mend is refused at once
		tick(1)
		if err := <-done; !errors.Is(err, model.ErrHPFull) {
			t.Fatalf("Mend at full HP: %v, want refused at once", err)
		}
		done = castAsync(inst, p.ID, "rally")
		tick(int(2 * turn))
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		used = eventsOf[model.SkillUsed](s)
		if len(used) != 3 || used[1].Tick != hits[0].Tick+turn-slack || used[2].Tick != hits[0].Tick+2*turn-slack {
			t.Fatalf("skills used at %+v; want Strike at %d and Rally at %d, each a turn after the last turn's end", used, hits[0].Tick+turn-slack, hits[0].Tick+2*turn-slack)
		}
	})
}

// A second use while one waits for the turn is refused with its own reason, not as cooling down, so the client gives that key its turn back without a cooldown.
func TestASecondUseWhileOneWaitsIsRefused(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, skillData(t), arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(100)
		p.MP = 1000
		join(t, inst, p)
		_ = inst.Attack(p.ID)
		tick(1)
		first := castAsync(inst, p.ID, "strike")
		tick(1)
		if err := cast(inst, p.ID, "rally"); !errors.Is(err, model.ErrSkillWaiting) {
			t.Fatalf("Rally while Strike waits: %v, want ErrSkillWaiting", err)
		}
		tick(20)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
	})
}

// A skill use still waiting for its turn when its player leaves hears it won't go.
func TestASkillWaitingForItsTurnIsAnsweredAtALeave(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, skillData(t), arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(100)
		join(t, inst, p)
		_ = inst.Attack(p.ID)
		tick(1)
		done := castAsync(inst, p.ID, "strike")
		tick(1)
		if err := inst.Leave(p.ID, "c1", nil); err != nil {
			t.Fatal(err)
		}
		tick(1)
		if err := <-done; !errors.Is(err, transient.ErrUnavailable) {
			t.Fatalf("waiting Strike answered %v, want unavailable", err)
		}
	})
}

// A skill's cooldown is cut by the caster's cooldown speed and counted in ticks. One used a little before it ends is let through, and the next cooldown counts from the end, so the early use gains nothing.
func TestASkillsCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := skillData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		p := caster(100)
		p.MP = 1000
		p.Points = model.Points{Spent: gamedata.Stats{INT: 200}} // cooldown speed 0.4 × 207 ÷ 407, about 0.2
		s := join(t, inst, p)
		warrior, _ := data.Class("warrior")
		speed := instance.StatsOf(data, warrior, 1, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived.CooldownSpeed
		cd := model.SkillCooldownTicks(2*time.Second, speed, worldtest.TickInterval)
		if cd >= 40 {
			t.Fatalf("a %d tick cooldown; want cooldown speed to cut 2 s's 40", cd)
		}
		const slack = 3 // 150 ms in 50 ms ticks
		usedAt := func() uint64 {
			t.Helper()
			for range 2 * cd {
				err := cast(inst, p.ID, "strike")
				if err == nil {
					used := eventsOf[model.SkillUsed](s)
					return used[len(used)-1].Tick
				}
				if !errors.Is(err, model.ErrSkillCoolingDown) {
					t.Fatal(err)
				}
			}
			t.Fatal("never used")
			return 0
		}
		first := usedAt()
		if second := usedAt(); second != first+cd-slack {
			t.Fatalf("used again at tick %d, want %d: %d ticks after, less the slack", second, first+cd-slack, cd)
		}
		if third := usedAt(); third != first+2*cd-slack {
			t.Fatalf("used a third time at tick %d, want %d: the second cooldown counts from the first's end", third, first+2*cd-slack)
		}
	})
}

// A map change carries when each cooldown and buff ends: Rally, used in town, is still cooling down on the next map, and its buff ends there when its time is up, the time in transit counted.
func TestAMapChangeCarriesCooldownsAndBuffs(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := skillData(t)
		tr := &transferRecorder{}
		town, stopTown := worldtest.Run(t, data, nil, worldtest.Options{Transfer: tr})
		defer stopTown()
		next, stopNext := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stopNext()
		tick(1)
		p := caster(0)
		at := data.StartTown().Portals[0].Arrival()
		p.X, p.Y = at.X, at.Y
		join(t, town, p)
		if err := cast(town, p.ID, "rally"); err != nil {
			t.Fatal(err)
		}
		tick(5)
		_ = town.EnterPortal(p.ID)
		tick(1)
		if moved(tr) != 1 {
			t.Fatal("the portal moved nobody")
		}
		timers := tr.moved[0].Timers
		cooldown, buff := time.Until(timers.Cooldowns["rally"]), timers.Buffs["rally"]
		if left := time.Until(buff.Until); cooldown <= 0 || cooldown >= time.Second || left <= time.Second || left >= 2*time.Second || buff.Points != 1 {
			t.Fatalf("carried %+v, want Rally part way through its 1 s cooldown and 2 s buff, at its 1 point", timers)
		}

		s := join(t, next, tr.moved[0])
		if err := cast(next, p.ID, "rally"); !errors.Is(err, model.ErrSkillCoolingDown) {
			t.Fatalf("Rally again on the next map: %v, want still cooling down", err)
		}
		ends := int(model.TicksIn(time.Until(buff.Until), worldtest.TickInterval))
		tick(ends - 1)
		if len(sheets(s)) != 0 {
			t.Fatal("the buff ended before its time")
		}
		tick(1)
		if changed := sheets(s); len(changed) != 1 || changed[0].Sheet.DerivedSkills.PhysicalAttack != 0 {
			t.Fatalf("stats changed %+v, want the sheet without Rally once its time is up", changed)
		}
	})
}

// A checkpoint keeps when each running cooldown and buff ends, on the wall clock, with each buff's points; a fresh join counts them on, and a timer that ended meanwhile is gone.
func TestACheckpointKeepsTheSkillTimersAndAFreshJoinCountsThemOn(t *testing.T) {
	t.Run("kept", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			cps := &worldtest.Checkpoints{}
			inst, stop := worldtest.RunMap(t, skillData(t), arena(time.Hour), cps, worldtest.Options{})
			defer stop()
			tick(1)
			p := caster(250)
			join(t, inst, p)
			if err := cast(inst, p.ID, "rally"); err != nil {
				t.Fatal(err)
			}
			cps.Drain()
			left := make(chan struct{})
			_ = inst.Leave(p.ID, "c1", left)
			<-left
			saved := cps.Drain()
			if len(saved) != 1 {
				t.Fatalf("checkpoints %+v, want the leave's", saved)
			}
			timers := saved[0].Timers
			cooldown, buff := time.Until(timers.Cooldowns["rally"]), timers.Buffs["rally"]
			if cooldown <= 0 || cooldown > time.Second || time.Until(buff.Until) <= time.Second || time.Until(buff.Until) > 2*time.Second || buff.Points != 1 || len(timers.Cooldowns) != 1 {
				t.Fatalf("saved %+v, want only Rally's 1 s cooldown and 2 s buff, at its 1 point", timers)
			}
		})
	})
	t.Run("counted on", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			inst, stop := worldtest.RunMap(t, skillData(t), arena(time.Hour), nil, worldtest.Options{})
			defer stop()
			tick(1)
			p := caster(250)
			now := time.Now()
			p.Timers = model.SkillTimers{
				Cooldowns: map[string]time.Time{"rally": now.Add(500 * time.Millisecond), "strike": now.Add(-time.Second)},
				Buffs:     map[string]model.BuffTimer{"rally": {Until: now.Add(time.Second), Points: 1}},
			}
			s := join(t, inst, p)
			if err := cast(inst, p.ID, "rally"); !errors.Is(err, model.ErrSkillCoolingDown) {
				t.Fatalf("Rally after the join: %v, want still cooling down", err)
			}
			if err := cast(inst, p.ID, "strike"); err != nil {
				t.Fatalf("Strike, whose cooldown ended before the join: %v", err)
			}
			tick(15)
			if len(sheets(s)) != 0 {
				t.Fatal("the buff ended before its time")
			}
			tick(10)
			if changed := sheets(s); len(changed) != 1 || changed[0].Sheet.DerivedSkills.PhysicalAttack != 0 {
				t.Fatalf("stats changed %+v, want the sheet without Rally once its second is up", changed)
			}
		})
	})
}
