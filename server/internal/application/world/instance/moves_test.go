package instance_test

import (
	"context"
	"errors"
	"math"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/mobs"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// moveData is skillData with C17's moving skills, the Beginner teaching them: Lunge dashes 4 m in 200 ms, Hop Back leaps and dodges for 10 s, Shadow Step blinks up to 8 m, and Shadow Hop is Shadow Step with no blow, for a town.
func moveData(t *testing.T) *gamedata.Data {
	t.Helper()
	return skillDataWith(t, map[string]func(string) string{
		"classes.yaml": func(s string) string {
			return edit(t, "guard, mend]", "guard, mend, test_lunge, hop_back, test_shadow_step, test_disengage, test_shadow_hop]")(s)
		},
		"skills.yaml": func(s string) string {
			return s + `  - id: test_lunge
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
  - id: test_disengage
    icon: lorc/run
    kind: active
    effect: damage
    damage: physical
    shape: {kind: arc, radius: 3, angle: 90, height: 1.75}
    energy: 2
    cooldown: 1s
    length: 600ms
    power: {base: 1}
    targets: 1
    move: {kind: leap, distance: 5, time: 300ms, toward: back}
    push: {distance: 2}
    stages:
      - {at: 0, name: Disengage, description: "Strikes, then leaps back."}
  - id: test_shadow_hop
    icon: lorc/ninja-heroic-stance
    kind: active
    effect: move
    energy: 2
    cooldown: 1s
    length: 600ms
    move: {kind: blink, distance: 8, toward: lock}
    stages:
      - {at: 0, name: Shadow Hop, description: "Steps behind the lock, as Shadow Step does, where a town lets it."}
`
		},
	})
}

// mover is a caster at z with a point in each moving skill.
func mover(z float64) model.Player {
	p := caster(z)
	for _, id := range []string{"test_lunge", "hop_back", "test_shadow_step", "test_disengage", "test_shadow_hop"} {
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
		done := useWay(inst, p.ID, "test_lunge", sim.Way{Z: 1, Length: 50}, seq)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if v, _ := s.Own(p.ID); v.State.Move != (sim.Move{}) {
			t.Fatalf("a move %+v before the input after the press", v.State.Move)
		}
		dash := sim.Dash(pressed, sim.Way{Z: 1, Length: 4}, 12, sim.StopAtMob, false, data.Movement())
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
		done := useWay(inst, p.ID, "test_lunge", sim.Way{Z: 1, Length: 4}, seq+1) // pressed during input 3, which the world hasn't had
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
			done := useWay(inst, p.ID, "test_lunge", way, 0)
			tick(1)
			if err := <-done; !errors.Is(err, model.ErrBadSkillWay) {
				t.Fatalf("way %+v: %v, want ErrBadSkillWay", way, err)
			}
		}
		if used := eventsOf[model.SkillUsed](s); len(used) != 0 {
			t.Fatalf("refused ways used %+v", used)
		}
		done := useWay(inst, p.ID, "test_lunge", sim.Way{X: 1 - 9e-7, Length: 2}, 0)
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
// C17: a hit at its action's start lands before the move, which starts from the input after the press: Disengage strikes the slime 1.5 m ahead, then leaps 5 m back, out of its 3 m arc.
func TestAHitAtTheStartLandsBeforeItsLeap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, p, s, _, _, stop := moving(t, nextToSlime)
		defer stop()
		seq := worldtest.Walk(inst, p.ID, 0, sim.Input{}, 1)
		done := useWay(inst, p.ID, "test_disengage", sim.Way{Z: -1, Length: 5}, seq)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		worldtest.Walk(inst, p.ID, seq, sim.Input{}, 8)
		used := eventsOf[model.SkillUsed](s)[0].Tick
		if blows := blowsBy(s, p.ID); len(blows) != 1 || blows[0].Tick != used || !blows[0].Target.IsMob() {
			t.Fatalf("blows %+v, want one on the slime at the use's tick %d", blows, used)
		}
		if v, _ := s.Own(p.ID); math.Abs(nextToSlime-v.State.Z-5) > 1e-9 {
			t.Fatalf("leapt from z %v to %v, want 5 m back", nextToSlime, v.State.Z)
		}
	})
}

func TestABlinkPassesThroughAMob(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, p, s, _, _, stop := moving(t, nextToSlime)
		defer stop()
		seq := worldtest.Walk(inst, p.ID, 0, right, 2)
		pressed := viewAfter(t, s, p.ID, seq)
		done := useWay(inst, p.ID, "test_shadow_step", sim.Way{Z: 1, Length: 8}, seq)
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

// walled is a town arena whose slime stands still at (arenaX, z), its circle a point, with boxes.
func walled(t *testing.T, z float64, boxes ...sim.Box) *gamedata.Map {
	t.Helper()
	g := worldtest.Flat(40)
	g.Boxes = boxes
	return still(worldtest.Gridded(t, &gamedata.Map{
		ID: "arena", Kind: gamedata.Hunting, Town: "willowmere", Geometry: g,
		Spawns:      []gamedata.Spawn{{Mob: "green_slime", Max: 1, Respawn: time.Hour, At: gamedata.Point{X: arenaX, Z: z}}},
		PlayerSpawn: gamedata.Point{X: arenaX, Z: spawnZ},
	}))
}

// wallFrom is a wall across the arena whose south face is at z.
func wallFrom(z float64) sim.Box {
	return sim.Box{X: arenaX, Z: z + 0.5, HalfW: 5, HalfD: 0.5, UX: 1, Top: 3}
}

// farSide is where a blink at a slime standing at z lands behind it: just past its far contact ring.
func farSide(z float64) float64 { return z + touching + 0.05 }

// blinkBehind presses Shadow Hop from (arenaX, from) at the slime on m, locked, the way aimed as the client aims it at the slime drawn miss metres nearer than it stands; it returns where the blink lands.
func blinkBehind(t *testing.T, m *gamedata.Map, from, miss float64) sim.State {
	t.Helper()
	var landed sim.State
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, moveData(t), m, nil, worldtest.Options{Transfer: &transferRecorder{}})
		defer stop()
		tick(1)
		p := mover(from)
		s := join(t, inst, p)
		slime, at := slimeOf(t, s), m.Spawns[0].At
		if slime.X != at.X || slime.Z != at.Z {
			t.Fatalf("the slime at (%v, %v); want it still at (%v, %v)", slime.X, slime.Z, at.X, at.Z)
		}
		seq := uint32(0)
		input := func() { // as a client sends it: naming the newest frame it drew
			seq++
			_ = inst.Input(p.ID, seq, sim.Input{}, uint32(s.Last().Tick)) //nolint:gosec // a test's ticks are few
			tick(1)
		}
		for range mobs.History + 1 { // the frames a view names kept from here
			input()
		}
		done := make(chan error, 1)
		go func() {
			way := sim.Way{Z: 1, Length: min(slime.Z-miss-from+touching, 8)}
			done <- inst.UseSkill(context.Background(), p.ID, "c1", model.SkillUse{Skill: "test_shadow_hop", Root: "test_shadow_hop", Way: way, Target: model.EntityRef{Mob: slime.ID}}, seq)
		}()
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		input()
		landed = viewAfter(t, s, p.ID, seq)
	})
	return landed
}

// A blink at the lock lands just past the locked mob's far side, as the frame the player's input names has it, wherever the client drew it. The client's own way, to the far contact ring as it drew the mob, landed in front when the mob stood nearer the world's way than drawn, as lag draws one walking away (the 4b-1 playtest's Shadow Dance), and when its sum rounded a hair short of the ring, as one in ten does from a slime off a whole metre; drawn farther, it landed further behind.
func TestABlinkAtTheLockLandsBehindIt(t *testing.T) {
	for _, tc := range []struct {
		name        string
		slime, from float64
		miss        float64
	}{
		{"drawn where it stands", slimeZ, slimeZ - 3.7, 0},
		{"drawn where it stands, its sum short of the ring", 20.3, 15.97, 0},
		{"drawn nearer, as one walking away", slimeZ, slimeZ - 3.7, 0.3},
		{"drawn farther, as one coming", slimeZ, slimeZ - 3.7, -0.3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := blinkBehind(t, walled(t, tc.slime), tc.from, tc.miss); got.X != arenaX || math.Abs(got.Z-farSide(tc.slime)) > 1e-9 {
				t.Fatalf("landed at (%v, %v); want (%v, %v), just past the slime's far side", got.X, got.Z, arenaX, farSide(tc.slime))
			}
		})
	}
}

// "Within 8 m" is the foe: a blink at a lock 7.9 m off lands behind it, past the skill's 8 m, where the client's way, held to 8 m, ended in its body and landed in front. A lock past 8 m isn't one it reaches: the blink goes the client's way.
func TestABlinkAtALockAtTheSkillsRangeLandsBehindIt(t *testing.T) {
	if got := blinkBehind(t, walled(t, slimeZ), slimeZ-7.9, 0); math.Abs(got.Z-farSide(slimeZ)) > 1e-9 {
		t.Fatalf("landed at z %v from 7.9 m off; want %v, just past the slime's far side", got.Z, farSide(slimeZ))
	}
	if got := blinkBehind(t, walled(t, slimeZ), slimeZ-9, 0); math.Abs(got.Z-(slimeZ-1)) > 1e-9 {
		t.Fatalf("landed at z %v from 9 m off; want %v, the client's 8 m", got.Z, slimeZ-1)
	}
}

// A blink at a lock with a wall behind it lands behind when the body fits there, and in front, at the last point clear of the mob on its way, when it doesn't.
func TestABlinkAtALockByAWall(t *testing.T) {
	room := walled(t, slimeZ, wallFrom(farSide(slimeZ)+0.375+0.1))
	if got := blinkBehind(t, room, slimeZ-4, 0); math.Abs(got.Z-farSide(slimeZ)) > 1e-9 {
		t.Fatalf("landed at z %v with room behind; want %v, just past the slime's far side", got.Z, farSide(slimeZ))
	}
	if got := blinkBehind(t, walled(t, slimeZ, wallFrom(slimeZ+touching+0.1)), slimeZ-4, 0); got.Z > slimeZ-touching {
		t.Fatalf("landed at z %v with no room behind; want in front, short of the slime's near side at %v", got.Z, slimeZ-touching)
	}
}
