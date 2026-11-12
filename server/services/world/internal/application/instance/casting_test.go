package instance_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// At 50 ms ticks of 3 sub-steps: Strike's 1500 ms is 30 ticks, with its cancel point at 25, and Rally's 1050 ms is 21.
const (
	subSteps      = 3
	strikeCasting = 30 * subSteps
	rallyCasting  = 21 * subSteps
)

var (
	right = sim.Input{Right: true}
	left  = sim.Input{Left: true}
)

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

// walker is a caster with MP to spare on the arena, walked right for two inputs.
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
	p.MP = 1000
	s := join(t, inst, p)
	seq := worldtest.Walk(inst, p.ID, 0, right, 2)
	return walker{inst: inst, p: p, s: s, seq: seq, full: viewAfter(t, s, p.ID, seq).VX}, stop
}

func TestCastingStartsAtTheInputAfterThePressWithTheFullLength(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, stop := walkingCaster(t, 800)
		defer stop()
		inst, p, s, seq := w.inst, w.p, w.s, w.seq
		done := useDuring(inst, p.ID, "strike", seq)
		tick(1) // Strike starts, with no input this tick
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if st := viewAfter(t, s, p.ID, seq); st.Casting != 0 || st.VX != w.full {
			t.Fatalf("before the press's next input: %+v, want walking at full speed", st)
		}
		seq = worldtest.Walk(inst, p.ID, seq, right, 1)
		if st := viewAfter(t, s, p.ID, seq); st.Casting != strikeCasting-subSteps || st.VX != w.full/2 {
			t.Fatalf("the press's next input: %+v, want casting %d at half speed", st, strikeCasting-subSteps)
		}
	})
}

func TestASkillTakenAfterItsNextInputCastsFromTheInputAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, stop := walkingCaster(t, 800)
		defer stop()
		inst, p, s, seq := w.inst, w.p, w.s, w.seq
		_ = inst.Input(p.ID, seq+1, right) // stepped before the skill starts in the same tick
		done := useDuring(inst, p.ID, "strike", seq)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		seq++
		if st := viewAfter(t, s, p.ID, seq); st.Casting != 0 || st.VX != w.full {
			t.Fatalf("the input stepped before the skill: %+v, want full speed", st)
		}
		seq = worldtest.Walk(inst, p.ID, seq, right, 1)
		if st := viewAfter(t, s, p.ID, seq); st.Casting != strikeCasting-subSteps || st.VX != w.full/2 {
			t.Fatalf("the next input: %+v, want the full casting from it", st)
		}
	})
}

func TestAnAttackCuttingASkillEndsItsCastingAtTheInputAfterItsPress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, stop := walkingCaster(t, 800)
		defer stop()
		inst, p, s, seq := w.inst, w.p, w.s, w.seq
		done := useDuring(inst, p.ID, "strike", seq)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		seq = worldtest.Walk(inst, p.ID, seq, right, 25) // to Strike's cancel point
		if st := viewAfter(t, s, p.ID, seq); st.Casting != strikeCasting-25*subSteps || st.VX != w.full/2 {
			t.Fatalf("at the cancel point: %+v, want still casting", st)
		}
		_ = inst.Attack(p.ID, seq)
		tick(1) // the attack cuts Strike short
		seq = worldtest.Walk(inst, p.ID, seq, right, 1)
		if st := viewAfter(t, s, p.ID, seq); st.Casting != 0 || st.VX != w.full {
			t.Fatalf("the input after the attack's press: %+v, want full speed", st)
		}
	})
}

func TestAnAttackWaitingForASkillLeavesItsCastingToRunOutAtItsLength(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, stop := walkingCaster(t, 800)
		defer stop()
		inst, p, s, seq := w.inst, w.p, w.s, w.seq
		done := useDuring(inst, p.ID, "rally", seq)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		_ = inst.Attack(p.ID, seq) // Rally can't be cut short, so it waits for its end
		seq = worldtest.Walk(inst, p.ID, seq, right, rallyCasting/subSteps)
		if st := viewAfter(t, s, p.ID, seq); st.Casting != 0 || st.VX != w.full/2 {
			t.Fatalf("Rally's last input: %+v, want its casting run out over it", st)
		}
		seq = worldtest.Walk(inst, p.ID, seq, right, 1)
		if st := viewAfter(t, s, p.ID, seq); st.VX != w.full {
			t.Fatalf("after Rally: %+v, want full speed", st)
		}
	})
}

func TestCastingHoldsTheFacing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, stop := walkingCaster(t, 800)
		defer stop()
		inst, p, s, seq := w.inst, w.p, w.s, w.seq
		done := useDuring(inst, p.ID, "strike", seq)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		seq = worldtest.Walk(inst, p.ID, seq, left, 1)
		if st := viewAfter(t, s, p.ID, seq); st.Facing != 1 || st.VX != -w.full/2 {
			t.Fatalf("walking left while casting: %+v, want backward at half speed, facing right", st)
		}
	})
}

func TestARefusedSkillNeverCasts(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w, stop := walkingCaster(t, 800)
		defer stop()
		inst, p, s, seq := w.inst, w.p, w.s, w.seq
		done := useDuring(inst, p.ID, "power_shot", seq) // a Bowman's
		tick(1)
		if err := <-done; err == nil {
			t.Fatal("a skill the Warrior can't learn was used")
		}
		seq = worldtest.Walk(inst, p.ID, seq, right, 1)
		if st := viewAfter(t, s, p.ID, seq); st.Casting != 0 || st.VX != w.full {
			t.Fatalf("after the refusal: %+v, want full speed", st)
		}
	})
}

func TestDeathEndsTheCasting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inst, stop := worldtest.RunMap(t, skillData(t), arena(time.Hour), nil, worldtest.Options{Transfer: &transferRecorder{}})
		defer stop()
		tick(1)
		p := caster(260)
		p.HP = 1
		s := join(t, inst, p)
		done := useDuring(inst, p.ID, "rally", 0)
		tick(1)
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		worldtest.Walk(inst, p.ID, 0, right, 4) // onto the slime, which kills
		v, ok := s.Own(p.ID)
		if !ok || !v.Dead || v.State.Casting != 0 {
			t.Fatalf("at death: %+v, want dead with no casting", v)
		}
	})
}
