package instance_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// moveData is skillData with C17's moving skills, the Beginner teaching them: Lunge dashes 4 m in 200 ms, Hop Back leaps and dodges for 10 s, Shadow Step blinks up to 8 m.
func moveData(t *testing.T) *gamedata.Data {
	t.Helper()
	return skillDataWith(t, map[string]func(string) string{
		"classes.yaml": func(s string) string {
			return strings.Replace(s, "second_wind, mend]", "second_wind, mend, lunge, hop_back, shadow_step]", 1)
		},
		"skills.yaml": func(s string) string {
			return s + `  - id: lunge
    icon: lorc/sprint
    kind: active
    effect: damage
    damage: physical
    shape: {kind: arc, radius: 1.875, angle: 120, height: 1.75}
    energy: 2
    cooldown: 1s
    length: 1s
    power: {base: 1}
    move: {kind: dash, distance: 4, time: 200ms, toward: lock}
    stages:
      - {at: 0, name: Lunge, description: "Lunges at the lock."}
  - id: hop_back
    icon: lorc/dodging
    kind: active
    effect: move
    energy: 2
    cooldown: 1s
    length: 600ms
    move: {kind: leap, distance: 4, time: 300ms, toward: back, stop: false}
    dodge: 10s
    stages:
      - {at: 0, name: Hop Back, description: "Hops back, dodging."}
  - id: shadow_step
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
	})
}

// mover is a caster at z with a point in each moving skill.
func mover(z float64) model.Player {
	p := caster(z)
	for _, id := range []string{"lunge", "hop_back", "shadow_step"} {
		p.Skills.Points[id] = 1
	}
	return p
}

// useWay uses the skill going way, as pressed during input seq; the answer comes once a tick resolves it.
func useWay(inst *instance.Instance, id kernel.CharacterID, skill string, way sim.Way, seq uint32) <-chan error {
	done := make(chan error, 1)
	go func() {
		done <- inst.UseSkill(context.Background(), id, "c1", model.SkillUse{Skill: skill, Root: skill, Way: way}, seq)
	}()
	return done
}

// moving is a mover on the arena, walked right for two inputs, with its frames and the arena's ground.
func moving(t *testing.T, z float64) (*instance.Instance, model.Player, *worldtest.Sink, *gamedata.Data, sim.Geometry, func()) {
	t.Helper()
	data, m := moveData(t), arena(t, time.Hour)
	inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{Transfer: &transferRecorder{}})
	tick(1)
	p := mover(z)
	return inst, p, join(t, inst, p), data, m.Geometry, stop
}

// replayed is s run on by n inputs of still keys, three sub-steps each, as the world steps them over open ground.
func replayed(s sim.State, n int, g sim.Geometry, p sim.Params) sim.State {
	for range 3 * n {
		s = sim.Step(s, sim.Input{}, g, p)
	}
	return s
}

// C17: a dash starts from the first input after its press, as the client predicts it, and the world holds its length to the skill's distance.
func TestADashStartsFromTheInputAfterItsPress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, p, s, data, g, stop := moving(t, spawnZ)
		defer stop()
		seq := worldtest.Walk(inst, p.ID, 0, right, 2)
		pressed := viewAfter(t, s, p.ID, seq)
		done := useWay(inst, p.ID, "lunge", sim.Way{Z: 1, Length: 50}, seq)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if v, _ := s.Own(p.ID); v.State.Move != (sim.Move{}) {
			t.Fatalf("a move %+v before the input after the press", v.State.Move)
		}
		dash := sim.Dash(pressed, sim.Way{Z: 1, Length: 4}, 12, true, false, data.Movement())
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
		if got, want := viewAfter(t, s, p.ID, seq), replayed(dash, 1, g, data.Movement()); got != want {
			t.Fatalf("after the input past the press %+v, want the dash's first three sub-steps %+v", got, want)
		}
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 3)
		got := viewAfter(t, s, p.ID, seq)
		if want := replayed(dash, 4, g, data.Movement()); got != want || got.Move != (sim.Move{}) {
			t.Fatalf("as the dash ends %+v, want %+v", got, want)
		}
		if math.Abs(got.Z-pressed.Z-4) > 1e-9 {
			t.Fatalf("dashed %v m, want the skill's 4 for a length of 50", got.Z-pressed.Z)
		}
	})
}

// C17: a press during an input the world hasn't stepped yet still moves from the input after that one.
func TestAMoveWaitsForTheInputAfterItsPressAhead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, p, s, _, _, stop := moving(t, spawnZ)
		defer stop()
		seq := worldtest.Walk(inst, p.ID, 0, right, 2)
		done := useWay(inst, p.ID, "lunge", sim.Way{Z: 1, Length: 4}, seq+1) // pressed during input 3, which the world hasn't had
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		seq = worldtest.Walk(inst, p.ID, seq, right, 1)
		if got := viewAfter(t, s, p.ID, seq); got.Move != (sim.Move{}) || got.VZ != 0 {
			t.Fatalf("after input 3, the press's own: %+v, want walking still", got)
		}
		seq = worldtest.Walk(inst, p.ID, seq, right, 1)
		if got := viewAfter(t, s, p.ID, seq); got.Move.Left != 9 || got.VZ <= 0 {
			t.Fatalf("after input 4: %+v, want the dash three sub-steps in", got)
		}
	})
}

// C17: a moving skill's way is a unit vector, within 1e-6, and a length of 0 or more; a skill that doesn't move ignores it.
func TestAMovingSkillsWayIsAUnitVectorAndALength(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, p, s, _, _, stop := moving(t, spawnZ)
		defer stop()
		for _, way := range []sim.Way{{}, {X: 0.5, Z: 0.5, Length: 1}, {X: 1 + 2e-6, Length: 1}, {X: math.NaN(), Length: 1}, {Z: 1, Length: -1}, {Z: 1, Length: math.NaN()}} {
			done := useWay(inst, p.ID, "lunge", way, 0)
			tick(1)
			if err := <-done; !errors.Is(err, model.ErrBadSkillWay) {
				t.Fatalf("way %+v: %v, want ErrBadSkillWay", way, err)
			}
		}
		if used := eventsOf[model.SkillUsed](s); len(used) != 0 {
			t.Fatalf("refused ways used %+v", used)
		}
		done := useWay(inst, p.ID, "lunge", sim.Way{X: 1 - 9e-7, Length: 2}, 0)
		tick(1)
		if err := <-done; err != nil {
			t.Fatalf("a way 9e-7 off unit length: %v", err)
		}
		tick(20)
		done = useWay(inst, p.ID, "strike", sim.Way{X: 3, Length: -1}, 0)
		tick(1)
		if err := <-done; err != nil {
			t.Fatalf("Strike, which doesn't move, with a bad way: %v", err)
		}
	})
}

// C17: a blink lands at the start of the input after its press, through a mob's body to past it.
func TestABlinkPassesThroughAMob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, p, s, _, _, stop := moving(t, nextToSlime)
		defer stop()
		seq := worldtest.Walk(inst, p.ID, 0, right, 2)
		pressed := viewAfter(t, s, p.ID, seq)
		done := useWay(inst, p.ID, "shadow_step", sim.Way{Z: 1, Length: 8}, seq)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
		if got := viewAfter(t, s, p.ID, seq); got.Z != pressed.Z+8 || got.Z < slimeZ {
			t.Fatalf("after the input past the press at z %v, want 8 m on at %v, past the slime at %v", got.Z, pressed.Z+8, slimeZ)
		}
	})
}

// C17: a mob's attack passes through a dodge, with no damage, knockback or guard, and DODGE shows; frames tell the dodge's edges.
func TestAMobsAttackPassesThroughADodge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, p, s, _, _, stop := moving(t, nextToSlime)
		defer stop()
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{}, 1)
		done := useWay(inst, p.ID, "hop_back", sim.Way{Z: -1}, seq) // a hop in place, in the slime's reach
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if v, _ := s.Own(p.ID); !v.Dodging {
			t.Fatal("no dodge in the frame of the press's tick")
		}
		worldtest.Walk(inst, p.ID, seq, sim.Input{}, 12) // the hop, landed
		start, _ := s.Own(p.ID)
		if start.State.Y != 0 || start.State.X != arenaX || start.State.Z != nextToSlime {
			t.Fatalf("after the hop in place %+v, want back on the ground where it stood", start.State)
		}
		hits := mobHitsOn(t, s, p.ID, 2)
		for _, h := range hits {
			if !h.Dodge || h.Amount != 0 || h.Miss {
				t.Fatalf("mob attacks in the dodge %+v, want each a DODGE with no damage", hits)
			}
		}
		v, _ := s.Own(p.ID)
		if v.HP != start.HP || v.Invulnerable || v.State.X != start.State.X || v.State.Z != start.State.Z {
			t.Fatalf("after two dodged attacks %+v, want the HP, place and no guard of %+v", v, start)
		}
		tick(200) // the dodge's 10 s end
		if v, _ := s.Own(p.ID); v.Dodging {
			t.Fatal("still dodging after 10 s")
		}
		landed := len(hits)
		for range 60 {
			if all := mobHitsOn(t, s, p.ID, landed+1); !all[len(all)-1].Dodge {
				return
			}
			landed++
		}
		t.Fatal("no attack landed after the dodge")
	})
}
