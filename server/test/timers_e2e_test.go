package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

func TestSkillTimers(t *testing.T) {
	t.Parallel()
	t.Run("C5/a-fresh-login-keeps-a-running-cooldown-and-buff", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		token, id := s.CreateCharacter(t, "rhea", "Rhea", "warrior")
		if _, err := s.DB.Exec(context.Background(), `INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'war_cry', 1, now(), now())`, id); err != nil {
			t.Fatal(err)
		}
		p, _ := s.Join(t, token, id)
		p.Drain(300 * time.Millisecond)
		use := func(p *e2etest.Player, req string) *clientv1.Response {
			p.Send(wsconn.Message{ReqID: req, Data: &clientv1.UseSkill{Skill: "war_cry"}}) // a chain's root, so it stands on a key of its own
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
				m, ok := p.Read(time.Until(deadline))
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
			t.Fatalf("War Cry in town: %+v", r)
		}

		// A new login, as a reload makes, kicks the first and joins from its checkpoint.
		again, w := s.Welcome(t, "rhea", id)
		var cooldown, buff *clientv1.SkillTimer
		for _, st := range w.GetSkillTimers() {
			switch st.GetKind() {
			case clientv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN:
				cooldown = st
			case clientv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF:
				buff = st
			}
		}
		if cooldown.GetSkill() != "war_cry" || cooldown.GetLeftMs() <= 50_000 || cooldown.GetLeftMs() > 60_000 ||
			buff.GetSkill() != "war_cry" || buff.GetLeftMs() <= 20_000 || buff.GetLeftMs() > 30_000 || buff.GetPoints() != 1 {
			t.Fatalf("welcomed back with timers %v; want War Cry's 60 s cooldown and 30 s buff still running", w.GetSkillTimers())
		}
		if got := w.GetCharacter().GetSheet().GetDerivedSkills().GetPhysicalAttack(); got != 9 {
			t.Fatalf("welcomed back with %v physical attack from skills; want War Cry's 9", got)
		}
		again.Drain(300 * time.Millisecond)
		if r := use(again, "u-2"); r.GetOk() || r.GetCode() != code("SKILL_COOLING_DOWN") {
			t.Fatalf("War Cry after the new login: %+v; want still cooling down", r)
		}
	})
}
