package instance_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/modules/world/internal/application/instance"
	"finga/modules/world/internal/model"
	"finga/modules/world/worldtest"
)

var right = sim.Input{Right: true}

// useDuring uses the skill as pressed during input seq; the answer comes once a tick resolves it.
func useDuring(inst *instance.Instance, id kernel.CharacterID, skill string, seq uint32) <-chan error {
	done := make(chan error, 1)
	go func() { done <- inst.UseSkill(context.Background(), id, "c1", skill, seq) }()
	return done
}

// viewAfter is the player's own view in the frame that acknowledges input seq.
func viewAfter(t *testing.T, s *worldtest.Sink, id kernel.CharacterID, seq uint32) sim.State {
	t.Helper()
	for _, d := range s.Deltas() {
		if d.AckSeq != seq {
			continue
		}
		for _, p := range d.Players {
			if p.ID == id {
				return p.State
			}
		}
	}
	t.Fatalf("no view of the player after input %d", seq)
	return sim.State{}
}

// walker is a caster on the arena, walked right for two inputs.
type walker struct {
	inst *instance.Instance
	p    model.Player
	s    *worldtest.Sink
	seq  uint32  // the last input sent
	full float64 // its walking speed, with its move speed
}

func walkingCaster(t *testing.T, x float64) (walker, func()) {
	t.Helper()
	inst, stop := worldtest.RunMap(t, skillData(t), arena(time.Hour), nil, worldtest.Options{Transfer: &transferRecorder{}})
	tick(1)
	p := caster(x)
	s := join(t, inst, p)
	seq := worldtest.Walk(inst, p.ID, 0, right, 2)
	return walker{inst: inst, p: p, s: s, seq: seq, full: viewAfter(t, s, p.ID, seq).VX}, stop
}

// A skill leaves movement alone: from the input after its press, its user walks, turns and jumps at full speed while it runs.
func TestASkillLeavesMovementAlone(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, stop := walkingCaster(t, 800)
		defer stop()
		inst, p, s, seq := w.inst, w.p, w.s, w.seq
		done := useDuring(inst, p.ID, "strike", seq)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		seq = worldtest.Walk(inst, p.ID, seq, right, 1)
		if st := viewAfter(t, s, p.ID, seq); st.VX != w.full {
			t.Fatalf("walking right as Strike runs: %+v, want full speed", st)
		}
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{Left: true}, 1)
		if st := viewAfter(t, s, p.ID, seq); st.VX != -w.full || st.Facing != -1 {
			t.Fatalf("turning as Strike runs: %+v, want walking left at full speed", st)
		}
		seq = worldtest.Walk(inst, p.ID, seq, sim.Input{Left: true, Jump: true}, 1)
		if st := viewAfter(t, s, p.ID, seq); st.VY >= 0 || st.OnGround {
			t.Fatalf("jumping as Strike runs: %+v, want in the air, rising", st)
		}
		if used := eventsOf[model.SkillUsed](s); len(used) != 1 || s.Deltas()[len(s.Deltas())-1].Tick >= used[0].Tick+20 {
			t.Fatalf("skills used %+v, want Strike, its 20 ticks still running", used)
		}
	})
}
