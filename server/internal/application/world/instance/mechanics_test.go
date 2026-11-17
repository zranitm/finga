package instance_test

import (
	"context"
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

// mechanicsData is skillData with C17's other mechanics, the Beginner teaching them:
//   - Flurry hits twice, 1.5 s apart.
//   - Rain is an area at the lock within 20 m; Short Rain within 12.
//   - War Cry is a party's buff.
//   - Barrier is a shield of 20 × magic attack.
func mechanicsData(t *testing.T) *gamedata.Data {
	t.Helper()
	return skillDataWith(t, mechanicsEdits(t))
}

// mechanicsEdits are mechanicsData's edits to skillData.
func mechanicsEdits(t *testing.T) map[string]func(string) string {
	damage := func(id, shape, more string) string {
		return `  - id: ` + id + `
    icon: lorc/sword-wound
    kind: active
    effect: damage
    damage: physical
    shape: ` + shape + `
    energy: 1
    cooldown: 1s
    length: 1s
    power: {base: 0.1}
    targets: 6
` + more + `    stages:
      - {at: 0, name: ` + id + `, description: "A test skill."}
`
	}
	return map[string]func(string) string{
		"classes.yaml": func(s string) string {
			return edit(t, "break_free, mend]", "break_free, mend, test_flurry, rain, short_rain, test_war_cry, test_barrier]")(s)
		},
		"skills.yaml": func(s string) string {
			return s +
				damage("test_flurry", "{kind: arc, radius: 2.375, angle: 120, height: 1.75}", "    hits: {count: 2, every: 1500ms}\n") +
				damage("rain", "{kind: area, radius: 2, at: lock, reach: 20}", "") +
				damage("short_rain", "{kind: area, radius: 2, at: lock, reach: 12}", "") + `  - id: test_war_cry
    icon: lorc/shouting
    kind: active
    effect: buff
    shape: {kind: area, radius: 8}
    energy: 1
    cooldown: 1s
    length: 1s
    duration: 5s
    reach_party: true
    bonus: {physical_attack: {base: 8}}
    stages:
      - {at: 0, name: War Cry, description: "The party hits harder."}
  - id: test_barrier
    icon: lorc/bolt-shield
    kind: active
    effect: buff
    shape: {kind: area, radius: 1}
    energy: 1
    cooldown: 1s
    length: 1s
    duration: 5s
    shield: {power: {base: 20}}
    stages:
      - {at: 0, name: Barrier, description: "A shield."}
`
		},
	}
}

// adept is a caster at (x, z) with a point in each of mechanicsData's skills.
func adept(x, z float64) model.Player {
	p := caster(z)
	p.X = x
	for _, id := range []string{"test_flurry", "rain", "short_rain", "test_war_cry", "test_barrier"} {
		p.Skills.Points[id] = 1
	}
	return p
}

// useAt uses the skill aimed along aim at the locked mob target; the answer comes once a tick resolves it.
func useAt(inst *instance.Instance, id kernel.CharacterID, skill string, aim float32, target uint32) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- inst.UseSkill(context.Background(), id, "c1", model.SkillUse{Skill: skill, Root: skill, Aim: aim, Target: target}, 0)
	}()
	return done
}

// blowsBy are the player's blows, MISSes too, in order.
func blowsBy(s *worldtest.Sink, id kernel.CharacterID) []model.Damage {
	var out []model.Damage
	for _, d := range damages(s) {
		if d.Source.Player == id {
			out = append(out, d)
		}
	}
	return out
}

// C17: a skill's hits repeat on schedule, each from where its user stands then: Flurry's second strikes the slime its user walked to.
func TestARepeatedHitFollowsItsUser(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := worldtest.Gridded(t, &gamedata.Map{
			ID: "two", Kind: gamedata.Hunting, Town: "town", Geometry: worldtest.Flat(40),
			Spawns: []gamedata.Spawn{
				{Mob: "green_slime", Max: 1, Respawn: time.Hour, At: gamedata.Point{X: 16, Z: 20}, Radius: 0.01},
				{Mob: "green_slime", Max: 1, Respawn: time.Hour, At: gamedata.Point{X: 24, Z: 20}, Radius: 0.01},
			},
			PlayerSpawn: gamedata.Point{X: 16, Z: 3},
		})
		inst, stop := worldtest.RunMap(t, mechanicsData(t), m, nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := adept(16, nextToSlime)
		s := join(t, inst, p)
		var west, east uint64
		for id, mv := range mobsIn(s) {
			if mv.X < 20 {
				west = id
			} else {
				east = id
			}
		}
		done := useAt(inst, p.ID, "test_flurry", 0, 0)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		worldtest.Walk(inst, p.ID, 0, right, 32) // 8 m east, under the other slime
		worldtest.Walk(inst, p.ID, 32, sim.Input{}, 2)
		blows := blowsBy(s, p.ID)
		if len(blows) != 2 || blows[0].Target.Mob != west || blows[1].Target.Mob != east || blows[1].Tick-blows[0].Tick != 30 {
			t.Fatalf("Flurry's blows %+v, want the west slime then 30 ticks on the east one", blows)
		}
	})
}

// C17: an area at the lock centres on the locked mob within its reach, else at its reach along the aim; its SkillUsed names the lock, for the others' clients to centre it too.
func TestAnAreaAtTheLockCentresOnTheLockWithinReach(t *testing.T) {
	for _, c := range []struct {
		name, skill string
		locked, hit bool
	}{
		{"locked within reach", "rain", true, true},
		{"locked past reach", "short_rain", true, false},
		{"no lock: reach along the aim, past the slime", "rain", false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, mechanicsData(t), arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := adept(arenaX, spawnZ) // 17 m south of the slime, facing it
				s := join(t, inst, p)
				var lock uint32
				for id := range mobsIn(s) {
					if c.locked {
						lock = uint32(id) //nolint:gosec // mob ids are small
					}
				}
				done := useAt(inst, p.ID, c.skill, 0, lock)
				tick(1)
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if got := len(blowsBy(s, p.ID)) > 0; got != c.hit {
					t.Fatalf("struck the slime %v, want %v", got, c.hit)
				}
				if used := eventsOf[model.SkillUsed](s); len(used) != 1 || used[0].Target != lock {
					t.Fatalf("skill uses told %+v, want one naming the lock %d", used, lock)
				}
			})
		})
	}
}

// C17: a party buff reaches the caster and each member on the map within its radius, each with their own timer; a member past it and a stranger get none.
func TestAPartyBuffReachesTheMembersInItsRadius(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := mechanicsData(t)
		parties := &fakeParties{}
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{Parties: parties})
		defer stop()
		tick(1)
		a, near, far, stranger := adept(arenaX, 30), warriorAt(36), warriorAt(39), warriorAt(31)
		parties.parties = [][]kernel.CharacterID{{a.ID, near.ID, far.ID}}
		sinks := map[kernel.CharacterID]*worldtest.Sink{}
		for _, p := range []model.Player{a, near, far, stranger} {
			sinks[p.ID] = join(t, inst, p)
		}
		done := useAt(inst, a.ID, "test_war_cry", 0, 0)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		warrior, _ := data.Class("warrior")
		bare := func(p model.Player) float64 {
			return instance.StatsOf(data, warrior, 1, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived.PhysicalAttack
		}
		for _, c := range []struct {
			name   string
			p      model.Player
			buffed bool
		}{{"the caster", a, true}, {"a member 6 m off", near, true}, {"a member 9 m off", far, false}, {"a stranger 1 m off", stranger, false}} {
			changes := eventsOf[model.StatsChanged](sinks[c.p.ID])
			got := len(changes) > 0 && changes[len(changes)-1].Sheet.Derived.PhysicalAttack > bare(c.p)
			if got != c.buffed {
				t.Errorf("%s buffed %v, want %v", c.name, got, c.buffed)
			}
		}
		tick(100) // the buff's 5 s
		if changes := eventsOf[model.StatsChanged](sinks[near.ID]); len(changes) == 0 || changes[len(changes)-1].Sheet.Derived.PhysicalAttack != bare(near) {
			t.Fatal("the member's buff didn't end with its timer")
		}
	})
}

// C17: a shield of 20 × magic attack takes a mob's damage before HP, and shows in the player's view; a map change carries what's left.
func TestAShieldTakesAMobsDamageFirst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := mechanicsData(t)
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := adept(arenaX, nextToSlime)
		s := join(t, inst, p)
		done := useAt(inst, p.ID, "test_barrier", 0, 0)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		warrior, _ := data.Class("warrior")
		pool := int(math.Round(instance.StatsOf(data, warrior, 1, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived.MagicAttack * 20))
		before, _ := s.Own(p.ID)
		if before.Shield != pool || pool == 0 {
			t.Fatalf("shield %d, want %d", before.Shield, pool)
		}
		var landed []model.Damage
		for range 60 {
			if landed = slices.DeleteFunc(mobHitsOn(t, s, p.ID, 1), func(d model.Damage) bool { return d.Miss }); len(landed) > 0 {
				break
			}
			tick(1)
		}
		v, _ := s.Own(p.ID)
		if len(landed) == 0 || v.HP != before.HP || v.Shield != pool-landed[0].Amount || landed[0].Shielded != landed[0].Amount {
			t.Fatalf("after a hit of %+v: HP %d, shield %d; want HP %d and the shield of %d less the hit", landed, v.HP, v.Shield, before.HP, pool)
		}
	})
}

// C17: a blow past what's left of a shield takes the rest off HP, and its Damage says what the shield took; the next, with the shield gone, takes it all off HP.
func TestABlowPastTheShieldSaysWhatItTook(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, mechanicsData(t), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := adept(arenaX, nextToSlime)
		p.Timers = model.SkillTimers{Buffs: map[string]model.BuffTimer{"test_barrier": {Until: time.Now().Add(time.Hour), Points: 1, Shield: 1}}}
		s := join(t, inst, p)
		before, _ := s.Own(p.ID)
		landed := func(n int) []model.Damage {
			for range 120 {
				if got := slices.DeleteFunc(mobHitsOn(t, s, p.ID, 1), func(d model.Damage) bool { return d.Miss }); len(got) >= n {
					return got
				}
				tick(1)
			}
			t.Fatalf("fewer than %d blows landed", n)
			return nil
		}
		first := landed(1)[0]
		v, _ := s.Own(p.ID)
		if first.Amount < 2 || first.Shielded != 1 || v.HP != before.HP-first.Amount+1 || v.Shield != 0 {
			t.Fatalf("a blow %+v past a shield of 1: HP %d from %d, shield %d; want the shield to take 1 and HP the rest", first, v.HP, before.HP, v.Shield)
		}
		second := landed(2)[1]
		if second.Shielded != 0 {
			t.Fatalf("a blow %+v with no shield left says the shield took some", second)
		}
	})
}

// C17: a buff joined with keeps its shield's pool, as a map change hands it on.
func TestAShieldComesWithItsBuffTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, mechanicsData(t), arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := adept(arenaX, spawnZ)
		p.Timers = model.SkillTimers{Buffs: map[string]model.BuffTimer{"test_barrier": {Until: time.Now().Add(3 * time.Second), Points: 1, Shield: 40}}}
		s := join(t, inst, p)
		if v, _ := s.Own(p.ID); v.Shield != 40 {
			t.Fatalf("shield %d joined with a pool of 40", v.Shield)
		}
		tick(61)
		if v, _ := s.Own(p.ID); v.Shield != 0 {
			t.Fatalf("shield %d after its buff's 3 s", v.Shield)
		}
	})
}
