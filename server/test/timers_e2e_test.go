package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

func TestSkillTimers(t *testing.T) {
	t.Run("C5/a-fresh-login-keeps-a-running-cooldown-and-buff", func(t *testing.T) {
		s := newStack(t)
		token, id := s.createCharacter(t, "rhea", "Rhea", "warrior")
		if _, err := s.db.Exec(context.Background(), `INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'iron_body', 1, now(), now())`, id); err != nil {
			t.Fatal(err)
		}
		p, _ := s.join(t, token, id)
		p.drain(300 * time.Millisecond)
		use := func(p *player, req string) *clientv1.Response {
			p.send(wsconn.Message{ReqID: req, Data: &clientv1.UseSkill{Skill: "iron_body"}})
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
				m, ok := p.read(time.Until(deadline))
				if !ok {
					break
				}
				if r, isResponse := m.Data.(*clientv1.Response); isResponse && m.ReqID == req {
					return r
				}
			}
			t.Fatalf("no answer to %s", req)
			return nil
		}
		if r := use(p, "u-1"); !r.GetOk() {
			t.Fatalf("Iron Body in town: %+v", r)
		}

		// A new login, as a reload makes, kicks the first and joins from its checkpoint.
		again, w := s.welcome(t, "rhea", id)
		var cooldown, buff *clientv1.SkillTimer
		for _, st := range w.GetSkillTimers() {
			switch st.GetKind() {
			case clientv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN:
				cooldown = st
			case clientv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF:
				buff = st
			}
		}
		if cooldown.GetSkill() != "iron_body" || cooldown.GetLeftMs() <= 50_000 || cooldown.GetLeftMs() > 60_000 ||
			buff.GetSkill() != "iron_body" || buff.GetLeftMs() <= 20_000 || buff.GetLeftMs() > 30_000 || buff.GetPoints() != 1 {
			t.Fatalf("welcomed back with timers %v; want Iron Body's 60 s cooldown and 30 s buff still running", w.GetSkillTimers())
		}
		if got := w.GetCharacter().GetSheet().GetDerivedSkills().GetPhysicalDefense(); got != 22 {
			t.Fatalf("welcomed back with %v physical defense from skills; want Iron Body's 22", got)
		}
		again.drain(300 * time.Millisecond)
		if r := use(again, "u-2"); r.GetOk() || r.GetCode() != code("SKILL_COOLING_DOWN") {
			t.Fatalf("Iron Body after the new login: %+v; want still cooling down", r)
		}
	})
}
