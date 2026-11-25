package instance_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
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

// usedAt waits for a skill use to be answered and returns the tick it went, which answered it: up to castSlack before its action starts.
func usedAt(t *testing.T, s *worldtest.Sink, done <-chan error, most int) uint64 {
	t.Helper()
	for range most {
		tick(1)
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return s.Last().Tick
		default:
		}
	}
	t.Fatalf("not answered in %d ticks", most)
	return 0
}

// A skill cuts off a basic attack at the attack's cancel point, not before. It may go up to castSlack early, but it starts at the cancel point, shown and landing there, and the attack after it waits as long as ever: the Thief's cancel point, 3 ticks in, is all slack, and gains nothing.
func TestASkillCutsOffAnAttackAtItsCancelPoint(t *testing.T) {
	for _, class := range []kernel.Class{"warrior", "thief"} {
		t.Run(string(class), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := timelineData(t, strikeCancelable(t, false))
				inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := caster(nextToSlime)
				p.Class = class
				s := join(t, inst, p)
				_, cancel := attackTicks(t, data, p)
				strike, _ := skillTicks(t, data, "strike")

				_ = inst.Attack(p.ID, 0, model.EntityRef{})
				tick(1)
				first := hitsBy(s, p.ID)[0]
				early := max(first+cancel-slack, first+1) // the tick after the attack's, at the soonest
				if used := usedAt(t, s, castAsync(inst, p.ID, "strike"), int(cancel)); used != early {
					t.Fatalf("Strike went at %d, want %d: the attack's cancel point, less the slack", used, early)
				}
				_ = inst.Attack(p.ID, 0, model.EntityRef{})
				tick(int(cancel + strike))
				if got, want := hitsBy(s, p.ID), []uint64{first, first + cancel, first + cancel + strike}; !slices.Equal(got, want) {
					t.Fatalf("hits at %v, want %v: Strike's at the cancel point, and the next attack as Strike ends", got, want)
				}
				if used := eventsOf[model.SkillUsed](s); len(used) != 1 || used[0].Tick != first+cancel {
					t.Fatalf("Strike shown as %+v, want once at the cancel point %d", used, first+cancel)
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
				inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := caster(nextToSlime)
				s := join(t, inst, p)
				length, cancel := skillTicks(t, data, "strike")
				if length != 20 || cancel != 9 {
					t.Fatalf("Strike of %d ticks with its cancel point at %d, want 20 and 9: 450 ms in, as when it lasted 30", length, cancel)
				}

				if err := cast(inst, p.ID, "strike"); err != nil {
					t.Fatal(err)
				}
				_ = inst.Attack(p.ID, 0, model.EntityRef{})
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

// Neither kind cuts off its own: a skill waits for the last skill's end, going up to castSlack early but starting and landing at that end, and an attack for the last attack's end.
func TestTheSameKindWaitsForTheEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, strikeCancelable(t, true))
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(nextToSlime)
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
		_ = inst.Attack(p.ID, 0, model.EntityRef{}) // Strike is cancelable, so it goes at Strike's cancel point, counted from Rally's end
		tick(int(slack + strikeCancel))
		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		tick(int(attack))
		swing := first + rally + strikeCancel
		if got, want := hitsBy(s, p.ID), []uint64{first + rally, swing, swing + attack}; !slices.Equal(got, want) {
			t.Fatalf("hits at %v, want %v: Strike's at Rally's end", got, want)
		}
	})
}

// windUpAttack gives the Warrior's basic attack a hit at share of its length, and its cancel point there too, since a hit lands by it.
func windUpAttack(t *testing.T, share string) map[string]func(string) string {
	return map[string]func(string) string{"classes.yaml": edit(t, "radius: 2.375, angle: 120, height: 1.75, length:", "radius: 2.375, angle: 120, height: 1.75, hit: "+share+", cancel: "+share+", length:")}
}

// A wind-up hit lands in its tick from where the player stands then: walking into reach during it hits, standing out of reach doesn't.
func TestAWindUpHitLandsFromWhereThePlayerStandsThen(t *testing.T) {
	for _, walk := range []bool{true, false} {
		t.Run(map[bool]string{true: "walking into reach", false: "standing out of reach"}[walk], func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := timelineData(t, windUpAttack(t, "0.5"))
				inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := caster(slimeZ - 4.5) // out of a swing's 2.875 m, which 9 ticks' walk closes
				s := join(t, inst, p)
				length, _ := attackTicks(t, data, p)
				class, _ := data.Class(p.Class)
				hit, _ := model.ActionPoints(length, class.Attack.Hit, class.Attack.Cancel)

				_ = inst.Attack(p.ID, 0, model.EntityRef{})
				if walk {
					worldtest.Walk(inst, p.ID, 0, sim.Input{Up: true}, int(hit))
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
				inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{Transfer: &transferRecorder{}})
				defer stop()
				tick(1)
				watcher := join(t, inst, warriorAt(spawnZ))
				p := caster(nextToSlime)
				p.HP = 1
				s := join(t, inst, p)
				if !leave {
					untilTick(s, windupOn(t, s).Tick+15) // the slime's hop lands 20 ticks in, before the attack's hit
				}

				_ = inst.Attack(p.ID, 0, model.EntityRef{})
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

// A skill used early, within castSlack of an attack's cancel point, starts there, after the attack's own hit due in that tick, which still lands first.
func TestAnEarlySkillKeepsTheAttacksHitToLand(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		heavy := map[string]func(string) string{"skills.yaml": edit(t, "    cancel: 0.45\n    power: {base: 1.5,", "    cancel: 0.45\n    power: {base: 100,")} // Strike's blow known from the attack's by its size
		data := timelineData(t, withEdits(windUpAttack(t, "0.85"), heavy))
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(nextToSlime)
		s := join(t, inst, p)
		length, cancel := attackTicks(t, data, p)
		class, _ := data.Class(p.Class)
		if hit, _ := model.ActionPoints(length, class.Attack.Hit, class.Attack.Cancel); hit != cancel {
			t.Fatalf("the hit at %d, the cancel point at %d; want them together", hit, cancel)
		}

		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		tick(1)
		used := usedAt(t, s, castAsync(inst, p.ID, "strike"), int(cancel))
		tick(slack)
		at := used + slack // the cancel point
		if got := hitsBy(s, p.ID); !slices.Equal(got, []uint64{at, at}) {
			t.Fatalf("hits at %v, want the attack's and Strike's both at its cancel point %d", got, at)
		}
		var heard []string
		var blows []int
		for _, e := range s.All() {
			switch e := e.(type) {
			case model.Damage:
				if e.Source.Player == p.ID {
					heard, blows = append(heard, "hit"), append(blows, e.Amount)
				}
			case model.SkillUsed:
				if e.Caster == p.ID {
					heard = append(heard, "strike")
				}
			}
		}
		if want := []string{"hit", "strike", "hit"}; !slices.Equal(heard, want) || blows[1] < 10*blows[0] {
			t.Fatalf("the map heard %v, blows of %v, want %v: the attack's hit, then Strike starting and landing its heavy blow", heard, blows, want)
		}
	})
}

// A skill pressed past an attack's cancel point goes at once: shown and landing in the tick that answers it.
func TestASkillPressedPastTheCancelPointLandsAtOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, nil)
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(nextToSlime)
		s := join(t, inst, p)
		_, cancel := attackTicks(t, data, p)

		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		tick(1)
		hits := hitsBy(s, p.ID)
		if len(hits) != 1 {
			t.Fatalf("hits at %v, want the attack's as it starts", hits)
		}
		first := hits[0]
		tick(int(cancel))
		used := usedAt(t, s, castAsync(inst, p.ID, "strike"), 5)
		if used <= first+cancel {
			t.Fatalf("Strike went at %d, want past the cancel point %d", used, first+cancel)
		}
		if got := hitsBy(s, p.ID); !slices.Equal(got, []uint64{first, used}) {
			t.Fatalf("hits at %v, want the attack's at %d and Strike's at once, at %d", got, first, used)
		}
		if got := eventsOf[model.SkillUsed](s); len(got) != 1 || got[0].Tick != used {
			t.Fatalf("Strike shown as %+v, want once at %d", got, used)
		}
	})
}

// A skill with a wind-up spends its energy and starts its cooldown at the start and lands at its hit: its spend is told as the input after its press steps, before the hit.
func TestASkillsWindUpSpendsAtTheStartAndLandsLate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, strikeWith(t, "hit: 0.2"))
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(nextToSlime)
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

// A skill used with no action running, within castSlack before its own cooldown ends, still starts its action as the cooldown ends, landing there, so the attack after it waits from there.
func TestASkillEarlyOnItsCooldownCountsItsActionFromTheCooldownsEnd(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := timelineData(t, strikeCancelable(t, false))
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(nextToSlime)
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
		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		tick(int(length + slack))
		if got, want := hitsBy(s, p.ID), []uint64{first, first + cd, first + cd + length}; !slices.Equal(got, want) {
			t.Fatalf("hits at %v, want %v: the attack as Strike ends, counted from its cooldown's end", got, want)
		}
	})
}

// Each basic attack tells the map it started, for drawing: who, which way, and its length, the class's.
func TestAnAttackTellsTheMapItsLength(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(nextToSlime)
		length, _ := attackTicks(t, data, p)
		s := join(t, inst, p)
		watcher := join(t, inst, worldtest.NewPlayer("Watcher", arenaX, slimeZ+15))
		_ = inst.Attack(p.ID, 0, model.EntityRef{})
		tick(1)
		for _, sink := range []*worldtest.Sink{s, watcher} {
			if got := eventsOf[model.AttackUsed](sink); len(got) != 1 || got[0].Attacker != p.ID || got[0].Aim != 0 || got[0].Length != length {
				t.Fatalf("attacks told %+v; want one by %s aimed north, %d ticks long", got, p.ID, length)
			}
		}
	})
}

// An action's aim, the yaw it was pressed at, is told with it and on the player's view until the next action.
func TestAnActionTellsItsAim(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
		defer stop()
		tick(1)
		p := caster(nextToSlime)
		s := join(t, inst, p)
		_ = inst.Attack(p.ID, 1.25, model.EntityRef{})
		tick(1)
		if got := eventsOf[model.AttackUsed](s); len(got) != 1 || got[0].Aim != 1.25 {
			t.Fatalf("attacks told %+v, want one aimed at 1.25", got)
		}
		if v, _ := s.Own(p.ID); v.Aim != 1.25 {
			t.Fatalf("view aim %v, want the attack's 1.25", v.Aim)
		}
		done := make(chan error, 1)
		go func() {
			done <- inst.UseSkill(context.Background(), p.ID, "c1", model.SkillUse{Skill: "strike", Root: "strike", Aim: -1.5}, 0)
		}()
		used := usedAt(t, s, done, 120) // castSlack before the attack's cancel point
		v, _ := s.Own(p.ID)
		if got := eventsOf[model.SkillUsed](s); len(got) != 0 || v.Aim != 1.25 {
			t.Fatalf("skill uses told %+v and view aim %v at the press, before Strike starts; want none and the attack's 1.25", got, v.Aim)
		}
		tick(slack)
		if got := eventsOf[model.SkillUsed](s); len(got) != 1 || got[0].Aim != -1.5 || got[0].Tick != used+slack {
			t.Fatalf("skill uses told %+v, want one aimed at -1.5 as it starts, at %d", got, used+slack)
		}
		if v, _ := s.Own(p.ID); v.Aim != -1.5 {
			t.Fatalf("view aim %v, want the skill's -1.5", v.Aim)
		}
	})
}

// An attack is told before the Damage it causes in the same tick, so a watcher times the hit by the attack that made it.
func TestAnAttackIsToldBeforeItsHit(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    model.Player
	}{
		{"a shot", bowmanAt(farFromSlime)},
		{"a swing", warriorAt(nextToSlime)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				data := worldtest.LoadData(t)
				inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				join(t, inst, tc.p)
				watcher := join(t, inst, worldtest.NewPlayer("Watcher", arenaX, slimeZ+15))
				_ = inst.Attack(tc.p.ID, 0, model.EntityRef{})
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
