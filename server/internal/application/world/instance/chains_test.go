package instance_test

import (
	"context"
	"errors"
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

// chainData is mechanicsData with Hop Back (a hop dodging for 2 s), Daze (a 1 s stun on the mobs within 6 m), Strike hitting at 0.4 of its 1 s, and the Warrior's chains:
//   - Strike offers Flurry within 2 s of its hit, else Rain at a lock past 3 m, else Short Rain at a stunned or frozen lock;
//   - Hop Back offers Barrier within 3 s of a dodge, longer than its dodge.
func chainData(t *testing.T) *gamedata.Data {
	t.Helper()
	return skillDataWith(t, withEdits(mechanicsEdits(t), map[string]func(string) string{
		"classes.yaml": func(s string) string {
			s = edit(t, "test_war_cry, test_barrier]", "test_war_cry, test_barrier, hop_back, daze]")(s)
			return edit(t, "    skills: [slash_blast, iron_body, charge, ground_slam, war_cry, taunt, toughness, rally]", `    chains:
      - root: strike
        follow:
          - {skill: test_flurry, when: {after: 2s}}
          - {skill: rain, when: {far: 3}}
          - {skill: short_rain, when: {controlled: [stun, freeze]}}
      - root: hop_back
        follow:
          - {skill: test_barrier, when: {dodged: 3s}}
    skills: [slash_blast, iron_body, charge, ground_slam, war_cry, taunt, toughness, rally]`)(s)
		},
		"skills.yaml": func(s string) string {
			s = edit(t, "    length: 1000ms\n    cancelable: true\n    cancel: 0.45\n", "    length: 1000ms\n    cancelable: true\n    hit: 0.4\n    cancel: 0.45\n")(s)
			return s + `  - id: hop_back
    icon: lorc/dodging
    kind: active
    effect: move
    energy: 1
    cooldown: 1s
    length: 600ms
    move: {kind: leap, distance: 4, time: 300ms, toward: back, stop: false}
    dodge: 2s
    stages:
      - {at: 0, name: Hop Back, description: "Hops back, dodging."}
  - id: daze
    icon: lorc/shouting
    kind: active
    effect: status
    shape: {kind: area, radius: 6}
    energy: 1
    cooldown: 1s
    length: 1050ms
    status: {id: stun, time: 1s}
    stages:
      - {at: 0, name: Daze, description: "Stuns."}
`
		},
	}))
}

// chainer is a warrior caster at (x, z) with a point in each of chainData's skills.
func chainer(x, z float64) model.Player {
	p := adept(x, z)
	p.Skills.Points["hop_back"], p.Skills.Points["daze"] = 1, 1
	return p
}

// press uses skill from root's key, as pressed during input seq, and returns the answer once a tick resolves it, waiting at most 10 s for its turn.
func press(t *testing.T, inst *instance.Instance, id kernel.CharacterID, skill, root string, target uint32, seq uint32) error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- inst.UseSkill(context.Background(), id, "c1", model.SkillUse{Skill: skill, Root: root, Target: target}, seq)
	}()
	for range 200 {
		tick(1)
		select {
		case err := <-done:
			return err
		default:
		}
	}
	t.Fatalf("%s from %s's key, pressed during input %d: no answer in 10 s", skill, root, seq)
	return nil
}

// C18: a skill goes from its own key when it roots a chain or is in none, and a follow-up only from its root's key.
func TestAFollowUpGoesOnlyFromItsRootsKey(t *testing.T) {
	for _, c := range []struct {
		name, skill, root string
		ok                bool
	}{
		{"a root on its own key", "strike", "strike", true},
		{"a skill in no chain on its own key", "test_war_cry", "test_war_cry", true},
		{"a follow-up on its own key", "rain", "rain", false},
		{"a follow-up with no root named", "rain", "", false},
		{"a follow-up from another root's key", "rain", "hop_back", false},
		{"a follow-up from a key whose skill roots no chain", "rain", "test_war_cry", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, chainData(t), arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := chainer(arenaX, slimeZ-5) // 5 m from the slime: Rain's far holds
				s := join(t, inst, p)
				var lock uint32
				for id := range mobsIn(s) {
					lock = uint32(id) //nolint:gosec // mob ids are small
				}
				err := press(t, inst, p.ID, c.skill, c.root, lock, 0)
				if (err == nil) != c.ok || err != nil && !errors.Is(err, model.ErrChainNotReady) {
					t.Fatalf("%v, want ok %v, else ErrChainNotReady", err, c.ok)
				}
			})
		})
	}
}

// C18: Flurry follows Strike from 150 ms before its hit to 2 s and 150 ms after, counted in inputs from Strike's press, as the client counts them.
func TestAnAfterFollowUpHoldsWithinItsWindow(t *testing.T) {
	data := chainData(t)
	strike, _ := data.Skill("strike")
	st := strike.Stages[0]
	hitAt, _ := model.ActionPoints(model.TicksIn(st.Length, worldtest.TickInterval), st.Hit, st.Cancel)
	if hitAt != 8 {
		t.Fatalf("Strike hits on tick %d, want 8", hitAt)
	}
	for _, c := range []struct {
		name  string
		after int64 // inputs from Strike's hit to the press
		ok    bool
	}{
		{"4 inputs before the hit", -4, false},
		{"3 inputs before the hit", -3, true},
		{"at the hit", 0, true},
		{"2 s and 3 inputs after", 43, true},
		{"2 s and 4 inputs after", 44, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, data, arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := chainer(arenaX, spawnZ)
				join(t, inst, p)
				seq := worldtest.Walk(inst, p.ID, 0, sim.Input{}, 1)
				if err := press(t, inst, p.ID, "strike", "strike", 0, seq); err != nil {
					t.Fatal(err)
				}
				at := int64(seq) + int64(hitAt) + c.after //nolint:gosec // a few ticks
				if at <= int64(seq) {
					t.Fatalf("the press at input %d isn't past Strike's, %d", at, seq)
				}
				worldtest.Walk(inst, p.ID, seq, sim.Input{}, int(at-int64(seq)))
				err := press(t, inst, p.ID, "test_flurry", "strike", 0, uint32(at)) //nolint:gosec // a few inputs
				if (err == nil) != c.ok || err != nil && !errors.Is(err, model.ErrChainNotReady) {
					t.Fatalf("Flurry %d inputs from Strike's hit: %v, want ok %v", c.after, err, c.ok)
				}
			})
		})
	}
}

// C18: Rain follows Strike at a lock farther than 3 m less the 0.5 m slack, across the ground; with no lock it isn't offered.
func TestAFarFollowUpHoldsPastItsDistance(t *testing.T) {
	for _, c := range []struct {
		name   string
		off    float64 // from the slime's middle
		locked bool
		ok     bool
	}{
		{"2.6 m off", 2.6, true, true},
		{"2.4 m off", 2.4, true, false},
		{"no lock", 5, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, chainData(t), arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := chainer(arenaX, slimeZ-c.off)
				s := join(t, inst, p)
				var lock uint32
				for id := range mobsIn(s) {
					if c.locked {
						lock = uint32(id) //nolint:gosec // mob ids are small
					}
				}
				err := press(t, inst, p.ID, "rain", "strike", lock, 0)
				if (err == nil) != c.ok || err != nil && !errors.Is(err, model.ErrChainNotReady) {
					t.Fatalf("Rain %v m off, locked %v: %v, want ok %v", c.off, c.locked, err, c.ok)
				}
			})
		})
	}
}

// C18: Barrier follows Hop Back within 3 s and 150 ms of the last dodge, counted in inputs from the one its frame acked; with no dodge it isn't offered.
func TestADodgedFollowUpHoldsWithinItsWindow(t *testing.T) {
	for _, c := range []struct {
		name  string
		dodge bool
		after uint32 // inputs from the dodge's frame's ack to the press
		ok    bool
	}{
		{"no dodge", false, 0, false},
		{"3 s and 3 inputs after", true, 63, true},
		{"3 s and 4 inputs after", true, 64, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, chainData(t), arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := chainer(arenaX, nextToSlime)
				s := join(t, inst, p)
				seq := worldtest.Walk(inst, p.ID, 0, sim.Input{}, 1)
				if !c.dodge {
					if err := press(t, inst, p.ID, "test_barrier", "hop_back", 0, seq); !errors.Is(err, model.ErrChainNotReady) {
						t.Fatalf("Barrier with no dodge: %v, want ErrChainNotReady", err)
					}
					return
				}
				done := useWay(inst, p.ID, "hop_back", sim.Way{Z: -1}, seq) // a hop in place, in the slime's reach
				tick(1)
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				for v, _ := s.Own(p.ID); v.Dodging; v, _ = s.Own(p.ID) { // the dodge's 2 s
					seq = worldtest.Walk(inst, p.ID, seq, sim.Input{}, 1)
				}
				var last uint64
				for _, d := range damages(s) {
					if d.Dodge && d.Target.Player == p.ID {
						last = d.Tick
					}
				}
				if last == 0 {
					t.Fatal("no attack dodged")
				}
				var acked uint32
				for _, d := range s.Deltas() {
					if d.Tick == last {
						acked = d.AckSeq
					}
				}
				at := acked + c.after
				if at < seq {
					t.Fatalf("the press at input %d is behind the world's %d: the dodge came too late in the test", at, seq)
				}
				worldtest.Walk(inst, p.ID, seq, sim.Input{}, int(at-seq))
				err := press(t, inst, p.ID, "test_barrier", "hop_back", 0, at)
				if (err == nil) != c.ok || err != nil && !errors.Is(err, model.ErrChainNotReady) {
					t.Fatalf("Barrier %d inputs after the dodge's frame: %v, want ok %v", c.after, err, c.ok)
				}
			})
		})
	}
}

// C18: Short Rain follows Strike while its lock is stunned; a lock never stunned, one whose stun ended long before, and no lock don't offer it.
func TestAControlledFollowUpHoldsWhileTheLockIs(t *testing.T) {
	for _, c := range []struct {
		name          string
		dazed, locked bool
		wait          int // ticks from Daze's use to the press
		ok            bool
	}{
		{"a stunned lock", true, true, 0, true},
		{"a lock never stunned", false, true, 0, false},
		{"a lock whose stun ended 1 s before", true, true, 40, false},
		{"a stunned mob not locked", true, false, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				inst, stop := worldtest.RunMap(t, chainData(t), arena(t, time.Hour), nil, worldtest.Options{})
				defer stop()
				tick(1)
				p := chainer(arenaX, slimeZ-4)
				s := join(t, inst, p)
				var lock uint32
				for id := range mobsIn(s) {
					if c.locked {
						lock = uint32(id) //nolint:gosec // mob ids are small
					}
				}
				if c.dazed {
					if err := press(t, inst, p.ID, "daze", "daze", 0, 0); err != nil {
						t.Fatal(err)
					}
				}
				tick(c.wait)
				err := press(t, inst, p.ID, "short_rain", "strike", lock, 0)
				if (err == nil) != c.ok || err != nil && !errors.Is(err, model.ErrChainNotReady) {
					t.Fatalf("Short Rain: %v, want ok %v", err, c.ok)
				}
			})
		})
	}
}
