package e2e_test

import (
	"math"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/sim"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// sprintRules is what the data says of a walk's sprint: combat's length in the world's ticks, the sub-steps a tick holds, those of walking to a sprint, and its speed times the walk's.
func sprintRules(t *testing.T, s *e2etest.Stack, hz uint32) (combat uint64, perTick, steps int, mult float64) {
	t.Helper()
	data, err := load.Dir(s.Data)
	if err != nil {
		t.Fatal(err)
	}
	move := data.Movement()
	interval := time.Second / time.Duration(hz)
	return uint64(math.Ceil(float64(data.Combat())/float64(interval) - 1e-9)), int(math.Round(interval.Seconds() / move.Dt)), sim.SprintSteps(move), move.Sprint.Speed
}

// large-regions.md section 7: a player walking out of combat with no key but the walk's breaks into a sprint after 2 s, the count and the speed on their own view.
func TestWalkingOutOfCombatBreaksIntoASprint(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	token, id := s.CreateCharacter(t, "dash", "Dash", "warrior")
	s.Place(t, id, "willowmere", e2etest.Spots(t).Start, 134, 0) // the town's open ground, a long way from any mob
	l, _ := s.Hello(t, token, id)
	combat, perTick, steps, mult := sprintRules(t, s, l.TickHz)
	rec := l.Record()
	east := &clientv1.Input{Right: true}
	l.During(15*time.Second, func(m wsconn.Message) bool {
		rec.Take(m)
		return rec.Through(uint64(steps/perTick) + 10)
	}, nil, func() error { return l.Input(east) })
	if fault := rec.Fault(combat, perTick, steps, mult); fault != "" {
		t.Fatalf("walking east out of combat: %s; the frames %+v", fault, rec.Paces)
	}
}

// The same after a fight: a hit puts the player in combat, where they walk on without a sprint until combat's last tick, and 2 s later, with no key, they sprint.
func TestWalkingOnAfterCombatEndsBreaksIntoASprint(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	token, id := s.CreateCharacter(t, "kite", "Kite", "warrior")
	s.Place(t, id, "willowmere", e2etest.Spots(t).SlimeField, 134, 0)
	l, _ := s.Hello(t, token, id)
	combat, perTick, steps, mult := sprintRules(t, s, l.TickHz)
	rec := l.Record()
	if !l.AttackUntil(20*time.Second, func(m wsconn.Message) bool {
		rec.Take(m)
		return rec.Hit()
	}) {
		t.Fatal("no Damage named the player within 20 s")
	}
	west := &clientv1.Input{Left: true} // away from the slimes
	l.During(40*time.Second, func(m wsconn.Message) bool {
		rec.Take(m)
		return rec.Through(combat + uint64(steps/perTick) + 10)
	}, nil, func() error { return l.Input(west) })
	if fault := rec.Fault(combat, perTick, steps, mult); fault != "" {
		t.Fatalf("walking west after a hit: %s; hits at %v, the frames %+v", fault, rec.Hits, rec.Paces)
	}
}
