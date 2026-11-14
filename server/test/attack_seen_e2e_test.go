package e2e_test

import (
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// C11: a second player sees the first's basic attacks, their length and the way they face.
func TestOthersSeeAPlayersAttacks(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, gameData(t, stillSlimes()...))
	token, id := s.createCharacter(t, "attacker", "Attacker", "beginner")
	s.place(t, id, "forest", 850, 608, 135, 0) // between the still slimes and the boulder that keeps the mushrooms away, so no mob reaches them
	watcherToken, watcherID := s.createCharacter(t, "watcher", "Watcher", "beginner")
	s.place(t, watcherID, "forest", 900, 608, 135, 0)
	p, _ := s.join(t, token, id)
	w, _ := s.join(t, watcherToken, watcherID)
	p.drain(300 * time.Millisecond)

	p.send(wsconn.Message{Seq: p.nextSeq(), Data: &clientv1.Attack{}})
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		m, ok := w.read(time.Until(deadline))
		if a, isAttack := m.Data.(*clientv1.AttackUsed); ok && isAttack && a.GetAttacker() == id {
			if a.GetLengthTicks() != 21 || a.GetFacing() != 1 {
				t.Fatalf("the watcher saw %+v, want the Beginner's 21 ticks, facing right", a)
			}
			return
		}
	}
	t.Fatal("the watcher never saw the attacker's attack")
}
