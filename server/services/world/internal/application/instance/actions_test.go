package instance_test

import (
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

const slack = 3 // castSlack's 150 ms in 50 ms ticks

// timelineData is skillData with a slime no blow kills, so every action's hit shows, and more edits after.
func timelineData(t *testing.T, more map[string]func(string) string) *gamedata.Data {
	t.Helper()
	return skillDataWith(t, withEdits(map[string]func(string) string{"mobs.yaml": edit(t, "hp: 34", "hp: 1000000")}, more))
}

// edit replaces the first old in a file with new, and fails the test if the file has none.
func edit(t *testing.T, old, new string) func(string) string {
	return func(s string) string {
		if !strings.Contains(s, old) {
			t.Fatalf("no %q to edit", old)
		}
		return strings.Replace(s, old, new, 1)
	}
}

// cancelableStrike marks Strike, the first active skill, cancelable.
func cancelableStrike(t *testing.T) map[string]func(string) string {
	return map[string]func(string) string{"skills.yaml": edit(t, "cooldown: 2s\n", "cooldown: 2s\n    cancelable: true\n")}
}

// attackTicks is p's basic attack in ticks, and its cancel point, at p's attack speed.
func attackTicks(t *testing.T, data *gamedata.Data, p model.Player) (length, cancel uint64) {
	t.Helper()
	class, _ := data.Class(p.Class)
	speed := instance.StatsOf(data, class, p.Level, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived.AttackSpeed
	length = model.AttackLengthTicks(class.Attack.Length, speed, worldtest.TickInterval)
	_, cancel = model.ActionPoints(length, class.Attack.Hit, class.Attack.Cancel)
	if cancel >= length {
		t.Fatalf("an attack of %d ticks with its cancel point at %d: nothing to cut off", length, cancel)
	}
	return length, cancel
}

// skillTicks is a skill's first stage in ticks, and its cancel point.
func skillTicks(t *testing.T, data *gamedata.Data, id string) (length, cancel uint64) {
	t.Helper()
	s, _ := data.Skill(id)
	st := s.Stages[0]
	length = model.SkillLengthTicks(st.Length, worldtest.TickInterval)
	_, cancel = model.ActionPoints(length, st.Hit, st.Cancel)
	if cancel >= length {
		t.Fatalf("%s of %d ticks with its cancel point at %d: nothing to cut off", id, length, cancel)
	}
	return length, cancel
}

// hitsBy are the ticks of id's blows, a MISS included.
func hitsBy(s *worldtest.Sink, id kernel.CharacterID) []uint64 {
	var out []uint64
	for _, d := range damages(s) {
		if d.Source.Player == id {
			out = append(out, d.Tick)
		}
	}
	return out
}

// usedAt waits for a skill use to be answered and returns the tick it went.
func usedAt(t *testing.T, s *worldtest.Sink, done <-chan error, most int) uint64 {
	t.Helper()
	for range most {
		tick(1)
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			used := eventsOf[model.SkillUsed](s)
			return used[len(used)-1].Tick
		default:
		}
	}
	t.Fatalf("not answered in %d ticks", most)
	return 0
}

// A skill cuts off a basic attack at the attack's cancel point, not before. It may go up to castSlack early, but its action counts from the cancel point, so the attack after it waits as long as ever.
func TestASkillCutsOffAnAttackAtItsCancelPoint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, nil)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(250)
		p.MP = 1000
		s := join(t, inst, p)
		_, cancel := attackTicks(t, data, p)
		strike, _ := skillTicks(t, data, "strike")

		_ = inst.Attack(p.ID)
		tick(1)
		first := hitsBy(s, p.ID)[0]
		if used := usedAt(t, s, castAsync(inst, p.ID, "strike"), int(cancel)); used != first+cancel-slack {
			t.Fatalf("Strike went at %d, want %d: the attack's cancel point, less the slack", used, first+cancel-slack)
		}
		_ = inst.Attack(p.ID)
		tick(int(cancel + strike))
		if got, want := hitsBy(s, p.ID), []uint64{first, first + cancel - slack, first + cancel + strike}; !slices.Equal(got, want) {
			t.Fatalf("hits at %v, want %v: the next attack as Strike ends, counted from the cancel point", got, want)
		}
	})
}

// A basic attack cuts off a cancelable skill at the skill's cancel point; one that isn't cancelable runs to its end.
func TestAnAttackCutsOffOnlyACancelableSkill(t *testing.T) {
	for _, cancelable := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancelable", false: "not cancelable"}[cancelable], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var more map[string]func(string) string
				if cancelable {
					more = cancelableStrike(t)
				}
				data := timelineData(t, more)
				inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := caster(250)
				s := join(t, inst, p)
				length, cancel := skillTicks(t, data, "strike")

				if err := cast(inst, p.ID, "strike"); err != nil {
					t.Fatal(err)
				}
				_ = inst.Attack(p.ID)
				tick(int(length))
				used := eventsOf[model.SkillUsed](s)[0].Tick
				want := used + length
				if cancelable {
					want = used + cancel
				}
				if got := hitsBy(s, p.ID); !slices.Equal(got, []uint64{used, want}) {
					t.Fatalf("hits at %v, want Strike at %d and the attack at %d", got, used, want)
				}
			})
		})
	}
}

// Neither kind cuts off its own: a skill waits for the last skill's end, up to castSlack early with its action counted from that end, and an attack for the last attack's end.
func TestTheSameKindWaitsForTheEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, cancelableStrike(t))
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(250)
		p.MP = 1000
		s := join(t, inst, p)
		attack, _ := attackTicks(t, data, p)
		rally, _ := skillTicks(t, data, "rally")
		_, strikeCancel := skillTicks(t, data, "strike")

		if err := cast(inst, p.ID, "rally"); err != nil {
			t.Fatal(err)
		}
		first := eventsOf[model.SkillUsed](s)[0].Tick
		if used := usedAt(t, s, castAsync(inst, p.ID, "strike"), int(rally)); used != first+rally-slack {
			t.Fatalf("Strike went at %d, want %d: Rally's end, less the slack", used, first+rally-slack)
		}
		_ = inst.Attack(p.ID) // Strike is cancelable, so it goes at Strike's cancel point, counted from Rally's end
		tick(int(slack + strikeCancel))
		_ = inst.Attack(p.ID)
		tick(int(attack))
		swing := first + rally + strikeCancel
		if got, want := hitsBy(s, p.ID), []uint64{first + rally - slack, swing, swing + attack}; !slices.Equal(got, want) {
			t.Fatalf("hits at %v, want %v", got, want)
		}
	})
}

// Attack speed shortens a basic attack, moving its cancel point, and leaves a skill's length and cancel point alone.
func TestAttackSpeedMovesOnlyTheAttacksCancelPoint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, cancelableStrike(t))
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(250)
		_, slow := attackTicks(t, data, p)
		p.Worn.Items[0].Stats.AttackSpeed = 100
		s := join(t, inst, p)
		_, fast := attackTicks(t, data, p)
		if fast >= slow {
			t.Fatalf("a cancel point at %d with attack speed, %d without; want it sooner", fast, slow)
		}
		_, strikeCancel := skillTicks(t, data, "strike")

		_ = inst.Attack(p.ID)
		tick(1)
		first := hitsBy(s, p.ID)[0]
		usedAt(t, s, castAsync(inst, p.ID, "strike"), int(fast))
		_ = inst.Attack(p.ID)
		tick(int(fast + strikeCancel))
		if got, want := hitsBy(s, p.ID), []uint64{first, first + fast - slack, first + fast + strikeCancel}; !slices.Equal(got, want) {
			t.Fatalf("hits at %v, want %v", got, want)
		}
	})
}

// windUpAttack gives the Warrior's basic attack a hit at share of its length.
func windUpAttack(t *testing.T, share string) map[string]func(string) string {
	return map[string]func(string) string{"classes.yaml": edit(t, "width: 64, height: 56, length:", "width: 64, height: 56, hit: "+share+", length:")}
}

// A wind-up hit lands in its tick from where the player stands then: walking into reach during it hits, standing out of reach doesn't.
func TestAWindUpHitLandsFromWhereThePlayerStandsThen(t *testing.T) {
	for _, walk := range []bool{true, false} {
		t.Run(map[bool]string{true: "walking into reach", false: "standing out of reach"}[walk], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := timelineData(t, windUpAttack(t, "0.5"))
				inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := caster(190)
				s := join(t, inst, p)
				length, _ := attackTicks(t, data, p)
				class, _ := data.Class(p.Class)
				hit, _ := model.ActionPoints(length, class.Attack.Hit, class.Attack.Cancel)

				_ = inst.Attack(p.ID)
				if walk {
					worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, int(hit))
				} else {
					tick(int(hit))
				}
				if got := hitsBy(s, p.ID); len(got) != 0 {
					t.Fatalf("hits at %v before the wind-up's end", got)
				}
				tick(1)
				got := hitsBy(s, p.ID)
				if walk && len(got) != 1 {
					t.Fatalf("hits at %v, want one as the wind-up ends", got)
				}
				if !walk && len(got) != 0 {
					t.Fatalf("hits at %v from out of reach", got)
				}
			})
		})
	}
}

// A hit still to land is dropped when its player dies or leaves the map.
func TestAWindUpHitIsDroppedAtADeathOrALeave(t *testing.T) {
	for _, leave := range []bool{false, true} {
		t.Run(map[bool]string{false: "death", true: "leave"}[leave], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := timelineData(t, windUpAttack(t, "0.5"))
				inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{Transfer: &transferRecorder{}})
				defer stop()
				tick(1)
				watcher := join(t, inst, warriorAt(100))
				p := caster(260)
				p.HP = 1
				join(t, inst, p)

				_ = inst.Attack(p.ID)
				if leave {
					tick(1)
					if err := inst.Leave(p.ID, "c1", nil); err != nil {
						t.Fatal(err)
					}
				} else {
					worldtest.Walk(inst, p.ID, 0, sim.Input{Right: true}, 2) // onto the slime, which kills
				}
				tick(20)
				if got := hitsBy(watcher, p.ID); len(got) != 0 {
					t.Fatalf("hits at %v after the player was gone", got)
				}
			})
		})
	}
}

// A skill used early, within castSlack of an attack's cancel point, lands at once and leaves the attack's hit still to land in its tick.
func TestAnEarlySkillKeepsTheAttacksHitToLand(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, windUpAttack(t, "0.85"))
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(250)
		s := join(t, inst, p)
		length, cancel := attackTicks(t, data, p)
		class, _ := data.Class(p.Class)
		if hit, _ := model.ActionPoints(length, class.Attack.Hit, class.Attack.Cancel); hit != cancel {
			t.Fatalf("the hit at %d, the cancel point at %d; want them together", hit, cancel)
		}

		_ = inst.Attack(p.ID)
		tick(1)
		used := usedAt(t, s, castAsync(inst, p.ID, "strike"), int(cancel))
		tick(slack)
		if got, want := hitsBy(s, p.ID), []uint64{used, used + slack}; !slices.Equal(got, want) {
			t.Fatalf("hits at %v, want Strike's at %d and the attack's at its cancel point %d", got, used, used+slack)
		}
	})
}

// A skill with a wind-up spends its MP and starts its cooldown at the start and lands at its hit.
func TestASkillsWindUpSpendsAtTheStartAndLandsLate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, map[string]func(string) string{"skills.yaml": edit(t, "cooldown: 2s\n", "cooldown: 2s\n    hit: 0.5\n")})
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(250)
		s := join(t, inst, p)
		strike, _ := data.Skill("strike")
		st := strike.Stages[0]
		length := model.SkillLengthTicks(st.Length, worldtest.TickInterval)
		hit, _ := model.ActionPoints(length, st.Hit, st.Cancel)

		if err := cast(inst, p.ID, "strike"); err != nil {
			t.Fatal(err)
		}
		used := eventsOf[model.SkillUsed](s)[0].Tick
		spent := eventsOf[model.StatsChanged](s)
		if len(spent) != 1 || spent[0].Tick != used || spent[0].MP != p.MP-st.MPAt(1) {
			t.Fatalf("stats changed %+v, want the MP spent at %d", spent, used)
		}
		tick(int(hit))
		if got := hitsBy(s, p.ID); !slices.Equal(got, []uint64{used + hit}) {
			t.Fatalf("hits at %v, want Strike's at %d", got, used+hit)
		}
	})
}
