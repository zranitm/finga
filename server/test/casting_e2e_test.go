package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// C11: a skill slows its user and holds their facing on the server, and a second player sees the first's basic attacks.
func TestASkillSlowsItsUserAndOthersSeeTheirAttacks(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, gameData(t, stillSlimes()...))
	token, id := s.createCharacter(t, "caster", "Caster", "beginner")
	if _, err := s.db.Exec(context.Background(), `INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 1, now(), now())`, id); err != nil {
		t.Fatal(err)
	}
	s.place(t, id, "forest", 850, 608, 135, 0) // between the still slimes and the boulder that keeps the mushrooms away, so no mob reaches the caster
	watcherToken, watcherID := s.createCharacter(t, "watcher", "Watcher", "beginner")
	s.place(t, watcherID, "forest", 900, 608, 135, 0)
	p, _ := s.join(t, token, id)
	w, _ := s.join(t, watcherToken, watcherID)
	p.drain(300 * time.Millisecond)

	seq := uint32(0)
	walk := func(right bool) {
		seq++
		p.send(wsconn.Message{Seq: seq, Data: &clientv1.Input{Right: right, Left: !right}})
		time.Sleep(50 * time.Millisecond)
	}
	// own is the caster's next view that walks the way given.
	own := func(right bool) *clientv1.PlayerView {
		t.Helper()
		for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
			v, ok := p.position(id, time.Until(deadline))
			if ok && (v.GetVx() > 0) == right && v.GetVx() != 0 {
				return v
			}
		}
		t.Fatalf("the caster never walked right=%t", right)
		return nil
	}

	for range 3 {
		walk(true)
	}
	full := own(true).GetVx()
	p.send(wsconn.Message{Seq: seq, ReqID: "strike", Data: &clientv1.UseSkill{Skill: "strike"}})
	if r := p.expect("Response"); r.ReqID != "strike" || !r.Data.(*clientv1.Response).GetOk() {
		t.Fatalf("Strike answered %+v", r)
	}
	walk(false)
	if v := own(false); v.GetVx() != -full/2 || v.GetFacing() != 1 || v.GetCasting() == 0 {
		t.Fatalf("walking left while casting: %+v, want %v px/s backward, facing right", v, -full/2)
	}

	p.send(wsconn.Message{Seq: seq, Data: &clientv1.Attack{}})
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		m, ok := w.read(time.Until(deadline))
		if a, isAttack := m.Data.(*clientv1.AttackUsed); ok && isAttack && a.GetAttacker() == id {
			if a.GetLengthTicks() != 21 || a.GetFacing() != 1 {
				t.Fatalf("the watcher saw %+v, want the Beginner's 21 ticks, facing right", a)
			}
			return
		}
	}
	t.Fatal("the watcher never saw the caster's attack")
}
