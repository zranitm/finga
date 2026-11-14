package e2e_test

import (
	"context"
	"math"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// Combat feel section 15: a skill roots its user on the server, a direction held through it moving them nowhere, and a second player sees the first's basic attacks.
func TestASkillRootsItsUserAndOthersSeeTheirAttacks(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, gameData(t, stillSlimes()...))
	token, id := s.createCharacter(t, "caster", "Caster", "beginner")
	if _, err := s.db.Exec(context.Background(), `INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 1, now(), now())`, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(context.Background(), `UPDATE characters SET energy = 2 WHERE id = $1`, id); err != nil { // Strike's cost
		t.Fatal(err)
	}
	s.place(t, id, "forest", 850, 608, 135, 0) // between the still slimes and the boulder that keeps the mushrooms away, so no mob reaches the caster
	watcherToken, watcherID := s.createCharacter(t, "watcher", "Watcher", "beginner")
	s.place(t, watcherID, "forest", 900, 608, 135, 0)
	p, _ := s.join(t, token, id)
	w, _ := s.join(t, watcherToken, watcherID)
	p.drain(300 * time.Millisecond)

	walk := func(right bool) {
		p.send(wsconn.Message{Seq: p.nextSeq(), Data: &clientv1.Input{Right: right, Left: !right}})
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
	p.send(wsconn.Message{Seq: p.seq, ReqID: "strike", Data: &clientv1.UseSkill{Skill: "strike"}}) // pressed during the last input
	if r := p.expect("Response"); r.ReqID != "strike" || !r.Data.(*clientv1.Response).GetOk() {
		t.Fatalf("Strike answered %+v", r)
	}
	for range 25 { // through Strike's 20 ticks
		walk(false)
	}
	var root *clientv1.PlayerView // a frame leaves out a player who didn't move, so the root shows once, as they stop
	for {
		v, ok := p.position(id, 3*time.Second)
		if !ok {
			t.Fatalf("no view after the root at %+v", root)
		}
		if root == nil {
			if v.GetCasting() > 0 {
				root = v
			}
			continue
		}
		if root.GetVx() != 0 || root.GetFacing() != 1 || v.GetCasting() > 0 || v.GetVx() != -full || math.Abs(root.GetX()-v.GetX()-full*0.05) > 1e-6 {
			t.Fatalf("rooted at %+v, then %+v: want standing facing right, then walking left from there once the root ends", root, v)
		}
		break
	}

	p.send(wsconn.Message{Seq: p.nextSeq(), Data: &clientv1.Attack{}})
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		m, ok := w.read(time.Until(deadline))
		if a, isAttack := m.Data.(*clientv1.AttackUsed); ok && isAttack && a.GetAttacker() == id {
			if a.GetLengthTicks() != 21 || a.GetFacing() != -1 {
				t.Fatalf("the watcher saw %+v, want the Beginner's 21 ticks, facing left as it walked after the root", a)
			}
			return
		}
	}
	t.Fatal("the watcher never saw the caster's attack")
}
