package e2e_test

import (
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// C11: a second player sees the first's basic attacks, their length and the yaw they aim along, which the attacker's view then carries.
func TestOthersSeeAPlayersAttacks(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, stillSlimes()...))
	token, id := s.CreateCharacter(t, "attacker", "Attacker", "beginner")
	s.Place(t, id, "forest", e2etest.OutOfSight, 135, 0) // where no mob sees them, the slimes standing still, so no mob reaches them
	watcherToken, watcherID := s.CreateCharacter(t, "watcher", "Watcher", "beginner")
	s.Place(t, watcherID, "forest", gamedata.Point{X: e2etest.OutOfSight.X, Z: e2etest.OutOfSight.Z + 1.5}, 135, 0)
	p, _ := s.Join(t, token, id)
	w, _ := s.Join(t, watcherToken, watcherID)
	p.Drain(300 * time.Millisecond)

	p.Send(wsconn.Message{Seq: p.NextSeq(), Data: &clientv1.Attack{Aim: 1.25}})
	attacked, aimed := false, false
	w.Await(5*time.Second, func(m wsconn.Message) bool {
		switch d := m.Data.(type) {
		case *clientv1.AttackUsed:
			if d.GetAttacker() == id {
				if d.GetLengthTicks() != 21 || d.GetAim() != 1.25 {
					t.Fatalf("the watcher saw %+v, want the Beginner's 21 ticks, aimed at 1.25", d)
				}
				attacked = true
			}
		case *clientv1.Delta:
			for _, v := range d.GetPlayers() {
				aimed = aimed || v.GetId() == id && v.GetAim() == 1.25
			}
		}
		return attacked && aimed
	})
	if !attacked || !aimed {
		t.Fatalf("the watcher saw the attack %v and the attacker's view aimed %v; want both", attacked, aimed)
	}
}
