package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// C14: a character starts with no energy, an attack that reaches a mob earns it, a skill spends it, and a rejoin keeps it.
func TestEnergyIsEarnedSpentAndKept(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, gameData(t, append(stillSlimes(), loneSlime())...))
	slime := groundSlime(t, s)
	token, id := s.createCharacter(t, "spark", "Spark", "beginner")
	if _, err := s.db.Exec(context.Background(), `INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 1, now(), now())`, id); err != nil {
		t.Fatal(err)
	}
	s.place(t, id, "forest", slime.GetX()-40, slime.GetY(), 135, 0) // in reach of a 48 px swing, its box clear of the slime's
	p := s.connect(t, id)
	if w, _ := p.hello(token); w.GetCharacter().GetEnergy() != 0 {
		t.Fatalf("a new character joins with %d energy, want none", w.GetCharacter().GetEnergy())
	}
	p.face(true)
	p.drain(300 * time.Millisecond)

	// press sends Strike during the last input sent, as the client does, and reads until its answer and the energy a Delta tells after it, in either order; -1 when none is told.
	press := func(req string) (*clientv1.Response, int) {
		t.Helper()
		p.send(wsconn.Message{Seq: p.seq, ReqID: req, Data: &clientv1.UseSkill{Skill: "strike"}})
		var answer *clientv1.Response
		told := -1
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline) && (answer == nil || (answer.GetOk() && told < 0)); {
			m, ok := p.read(100 * time.Millisecond)
			switch d := m.Data.(type) {
			case *clientv1.Response:
				if ok && m.ReqID == req {
					answer = d
				}
			case *clientv1.Delta:
				if ok && d.Energy != nil {
					told = int(d.GetEnergy())
				}
			}
		}
		if answer == nil {
			t.Fatalf("no answer to %s in 5 s", req)
		}
		return answer, told
	}
	// attack swings at the slime, during the last input sent too, and returns the energy the next Delta that carries one tells.
	attack := func() int {
		t.Helper()
		p.send(wsconn.Message{Seq: p.seq, Data: &clientv1.Attack{}})
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			m, ok := p.read(100 * time.Millisecond)
			if d, isDelta := m.Data.(*clientv1.Delta); ok && isDelta && d.Energy != nil {
				return int(d.GetEnergy())
			}
		}
		t.Fatal("no Delta told the energy in 5 s after the attack")
		return 0
	}

	if r, told := press("broke"); r.GetOk() || r.GetCode() != code("NOT_ENOUGH_ENERGY") || told >= 0 {
		t.Fatalf("Strike with no energy: %+v, energy told %d; want NOT_ENOUGH_ENERGY and none told", r, told)
	}
	if e := attack(); e != 2 {
		t.Fatalf("an attack on the slime left %d energy, want 2", e)
	}
	time.Sleep(1200 * time.Millisecond) // the Beginner's 1050 ms attack ends
	if r, told := press("paid"); !r.GetOk() || told != 0 {
		t.Fatalf("Strike with 2 energy: %+v, energy told %d; want it used and 0 left", r, told)
	}
	time.Sleep(1600 * time.Millisecond) // Strike's 1500 ms ends
	if e := attack(); e != 2 {
		t.Fatalf("a second attack left %d energy, want 2", e)
	}

	_, w := s.welcome(t, "spark", id)
	if e := w.GetCharacter().GetEnergy(); e != 2 {
		t.Fatalf("welcomed back with %d energy, want the 2 it left with", e)
	}
}
