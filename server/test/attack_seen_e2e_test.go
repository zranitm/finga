package e2e_test

import (
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/pkg/wsconn"
)

// C11: a second player sees the first's basic attacks, their length and the yaw they aim along, which the attacker's view then carries.
func TestOthersSeeAPlayersAttacks(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, gameData(t, stillSlimes()...))
	token, id := s.createCharacter(t, "attacker", "Attacker", "beginner")
	s.place(t, id, "forest", outOfSight, 135, 0) // where no mob sees them, the slimes standing still, so no mob reaches them
	watcherToken, watcherID := s.createCharacter(t, "watcher", "Watcher", "beginner")
	s.place(t, watcherID, "forest", gamedata.Point{X: outOfSight.X, Z: outOfSight.Z + 1.5}, 135, 0)
	p, _ := s.join(t, token, id)
	w, _ := s.join(t, watcherToken, watcherID)
	p.drain(300 * time.Millisecond)

	p.send(wsconn.Message{Seq: p.nextSeq(), Data: &clientv1.Attack{Aim: 1.25}})
	attacked, aimed := false, false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline) && (!attacked || !aimed); {
		m, ok := w.read(time.Until(deadline))
		if !ok {
			break
		}
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
	}
	if !attacked || !aimed {
		t.Fatalf("the watcher saw the attack %v and the attacker's view aimed %v; want both", attacked, aimed)
	}
}
