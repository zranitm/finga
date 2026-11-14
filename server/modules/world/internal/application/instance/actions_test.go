package instance_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"
	"finga/modules/world/internal/application/instance"
	"finga/modules/world/internal/model"
	"finga/modules/world/worldtest"
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

// strikeWith gives Strike a line more, such as a hit or cancelable.
func strikeWith(t *testing.T, line string) map[string]func(string) string {
	return map[string]func(string) string{"skills.yaml": edit(t, "  - id: strike\n", "  - id: strike\n    "+line+"\n")}
}

// strikeCancelable sets whether a basic attack may cut Strike short; Strike is the data's first skill marked cancelable.
func strikeCancelable(t *testing.T, on bool) map[string]func(string) string {
	return map[string]func(string) string{"skills.yaml": edit(t, "    cancelable: true\n", fmt.Sprintf("    cancelable: %t\n", on))}
}

// attackTicks is p's basic attack in ticks, and its cancel point.
func attackTicks(t *testing.T, data *gamedata.Data, p model.Player) (length, cancel uint64) {
	t.Helper()
	class, _ := data.Class(p.Class)
	length = model.TicksIn(class.Attack.Length, worldtest.TickInterval)
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
	length = model.TicksIn(st.Length, worldtest.TickInterval)
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

// A skill cuts off a basic attack at the attack's cancel point, not before. It may go up to castSlack early, but its action counts from the cancel point, so the attack after it waits as long as ever: the Thief's cancel point, 3 ticks in, is all slack, and gains nothing.
func TestASkillCutsOffAnAttackAtItsCancelPoint(t *testing.T) {
	for _, class := range []kernel.Class{"warrior", "thief"} {
		t.Run(string(class), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := timelineData(t, strikeCancelable(t, false))
				inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := caster(250)
				p.Class = class
				s := join(t, inst, p)
				_, cancel := attackTicks(t, data, p)
				strike, _ := skillTicks(t, data, "strike")

				_ = inst.Attack(p.ID, 0)
				tick(1)
				first := hitsBy(s, p.ID)[0]
				early := max(first+cancel-slack, first+1) // the tick after the attack's, at the soonest
				if used := usedAt(t, s, castAsync(inst, p.ID, "strike"), int(cancel)); used != early {
					t.Fatalf("Strike went at %d, want %d: the attack's cancel point, less the slack", used, early)
				}
				_ = inst.Attack(p.ID, 0)
				tick(int(cancel + strike))
				if got, want := hitsBy(s, p.ID), []uint64{first, early, first + cancel + strike}; !slices.Equal(got, want) {
					t.Fatalf("hits at %v, want %v: the next attack as Strike ends, counted from the cancel point", got, want)
				}
			})
		})
	}
}

// A basic attack cuts off a cancelable skill at the skill's cancel point; one that isn't cancelable runs to its end.
func TestAnAttackCutsOffOnlyACancelableSkill(t *testing.T) {
	for _, cancelable := range []bool{true, false} {
		t.Run(map[bool]string{true: "cancelable", false: "not cancelable"}[cancelable], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := timelineData(t, strikeCancelable(t, cancelable))
				inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := caster(250)
				s := join(t, inst, p)
				length, cancel := skillTicks(t, data, "strike")
				if length != 20 || cancel != 9 {
					t.Fatalf("Strike of %d ticks with its cancel point at %d, want 20 and 9: 450 ms in, as when it lasted 30", length, cancel)
				}

				if err := cast(inst, p.ID, "strike"); err != nil {
					t.Fatal(err)
				}
				_ = inst.Attack(p.ID, 0)
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
		data := timelineData(t, strikeCancelable(t, true))
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(250)
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
		_ = inst.Attack(p.ID, 0) // Strike is cancelable, so it goes at Strike's cancel point, counted from Rally's end
		tick(int(slack + strikeCancel))
		_ = inst.Attack(p.ID, 0)
		tick(int(attack))
		swing := first + rally + strikeCancel
		if got, want := hitsBy(s, p.ID), []uint64{first + rally - slack, swing, swing + attack}; !slices.Equal(got, want) {
			t.Fatalf("hits at %v, want %v", got, want)
		}
	})
}

// windUpAttack gives the Warrior's basic attack a hit at share of its length, and its cancel point there too, since a hit lands by it.
func windUpAttack(t *testing.T, share string) map[string]func(string) string {
	return map[string]func(string) string{"classes.yaml": edit(t, "width: 64, height: 56, length:", "width: 64, height: 56, hit: "+share+", cancel: "+share+", length:")}
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

				_ = inst.Attack(p.ID, 0)
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
				p := caster(nextToSlime)
				p.HP = 1
				s := join(t, inst, p)
				if !leave {
					untilTick(s, windupOn(t, s).Tick+15) // the slime's hop lands 20 ticks in, before the attack's hit
				}

				_ = inst.Attack(p.ID, 0)
				if leave {
					tick(1)
					if err := inst.Leave(p.ID, "c1", nil); err != nil {
						t.Fatal(err)
					}
				} else {
					untilDead(t, s, p.ID)
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

		_ = inst.Attack(p.ID, 0)
		tick(1)
		used := usedAt(t, s, castAsync(inst, p.ID, "strike"), int(cancel))
		tick(slack)
		if got, want := hitsBy(s, p.ID), []uint64{used, used + slack}; !slices.Equal(got, want) {
			t.Fatalf("hits at %v, want Strike's at %d and the attack's at its cancel point %d", got, used, used+slack)
		}
	})
}

// A skill with a wind-up spends its energy and starts its cooldown at the start and lands at its hit: its spend is told as the input after its press steps, before the hit.
func TestASkillsWindUpSpendsAtTheStartAndLandsLate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, strikeWith(t, "hit: 0.2"))
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(250)
		s := join(t, inst, p)
		strike, _ := data.Skill("strike")
		st := strike.Stages[0]
		length := model.TicksIn(st.Length, worldtest.TickInterval)
		hit, _ := model.ActionPoints(length, st.Hit, st.Cancel)

		if err := cast(inst, p.ID, "strike"); err != nil {
			t.Fatal(err)
		}
		used := eventsOf[model.SkillUsed](s)[0].Tick
		stepInput(inst, p.ID, 1)
		var spentAt uint64
		for _, d := range eventsOf[worldtest.Delta](s) {
			if d.Energy != nil && *d.Energy == p.Energy-st.Energy {
				spentAt = d.Tick
			}
		}
		if spentAt != used+1 || spentAt >= used+hit {
			t.Fatalf("%d energy told at tick %d, want at %d, as the input after Strike's press steps, before its hit at %d", p.Energy-st.Energy, spentAt, used+1, used+hit)
		}
		tick(int(hit) - 1)
		if got := hitsBy(s, p.ID); !slices.Equal(got, []uint64{used + hit}) {
			t.Fatalf("hits at %v, want Strike's at %d", got, used+hit)
		}
	})
}

// A skill used with no action running, within castSlack before its own cooldown ends, still starts its action as the cooldown ends, so the attack after it waits from there.
func TestASkillEarlyOnItsCooldownCountsItsActionFromTheCooldownsEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, strikeCancelable(t, false))
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(250)
		s := join(t, inst, p)
		warrior, _ := data.Class("warrior")
		strike, _ := data.Skill("strike")
		speed := instance.StatsOf(data, warrior, 1, p.Points, p.Skills, p.Worn, progression.ItemStats{}, worldtest.TickInterval).Derived.CooldownSpeed
		cd := model.SkillCooldownTicks(strike.Stages[0].Cooldown, speed, worldtest.TickInterval)
		length, _ := skillTicks(t, data, "strike")
		if cd <= length+slack {
			t.Fatalf("a %d tick cooldown on a %d tick Strike: an action would still run", cd, length)
		}

		if err := cast(inst, p.ID, "strike"); err != nil {
			t.Fatal(err)
		}
		first := eventsOf[model.SkillUsed](s)[0].Tick
		tick(int(cd - slack - 1))
		if err := cast(inst, p.ID, "strike"); err != nil {
			t.Fatal(err)
		}
		_ = inst.Attack(p.ID, 0)
		tick(int(length + slack))
		if got, want := hitsBy(s, p.ID), []uint64{first, first + cd - slack, first + cd + length}; !slices.Equal(got, want) {
			t.Fatalf("hits at %v, want %v: the attack as Strike ends, counted from its cooldown's end", got, want)
		}
	})
}

// Each basic attack tells the map it started, for drawing: who, which way, and its length, the class's.
func TestAnAttackTellsTheMapItsLength(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(250)
		length, _ := attackTicks(t, data, p)
		s := join(t, inst, p)
		watcher := join(t, inst, worldtest.NewPlayer("Watcher", 600, 608))
		_ = inst.Attack(p.ID, 0)
		tick(1)
		for _, sink := range []*worldtest.Sink{s, watcher} {
			if got := eventsOf[model.AttackUsed](sink); len(got) != 1 || got[0].Attacker != p.ID || got[0].Facing != 1 || got[0].Length != length {
				t.Fatalf("attacks told %+v; want one by %s facing right, %d ticks long", got, p.ID, length)
			}
		}
	})
}

// An attack is told before the Damage it causes in the same tick, so a watcher times the hit by the attack that made it.
func TestAnAttackIsToldBeforeItsHit(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    model.Player
	}{
		{"a shot", bowmanAt(100)},
		{"a swing", warriorAt(250)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				inst, stop := worldtest.RunMap(t, data, arena(time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				join(t, inst, tc.p)
				watcher := join(t, inst, worldtest.NewPlayer("Watcher", 600, 608))
				_ = inst.Attack(tc.p.ID, 0)
				tick(1)
				used, hit := -1, -1
				var usedAt, hitAt uint64
				for n, e := range watcher.All() {
					switch e := e.(type) {
					case model.AttackUsed:
						if e.Attacker == tc.p.ID {
							used, usedAt = n, e.Tick
						}
					case model.Damage:
						if e.Source.Player == tc.p.ID {
							hit, hitAt = n, e.Tick
						}
					}
				}
				if used < 0 || hit < 0 || hit < used || hitAt != usedAt {
					t.Fatalf("the attack told at %d (tick %d) and its Damage at %d (tick %d); want the attack first, in the same tick", used, usedAt, hit, hitAt)
				}
			})
		})
	}
}
