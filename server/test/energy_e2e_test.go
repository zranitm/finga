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

	// until reads until pick says done; 15 s only guards a hang, as a slow runner's ticks lag the wall clock.
	until := func(what string, pick func(wsconn.Message) bool) {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
			if m, ok := p.read(time.Until(deadline)); ok && pick(m) {
				return
			}
		}
		t.Fatalf("no %s within 15 s", what)
	}
	// told is the energy a message tells, or -1 when it tells none.
	told := func(m wsconn.Message) int {
		if d, ok := m.Data.(*clientv1.Delta); ok && d.Energy != nil {
			return int(d.GetEnergy())
		}
		return -1
	}
	// past waits for the frame of tick end or a later one; the world sends one every tick.
	past := func(end uint64) {
		t.Helper()
		until("frame past the action's end", func(m wsconn.Message) bool { _, ok := m.Data.(*clientv1.Delta); return ok && m.Tick >= end })
	}
	// ack sends an input and reads until a Delta acks it, returning the last energy told on the way, or -1.
	ack := func() int {
		t.Helper()
		seq, last := p.nextSeq(), -1
		p.send(wsconn.Message{Seq: seq, Data: &clientv1.Input{}})
		until("Delta acking the input", func(m wsconn.Message) bool {
			if n := told(m); n >= 0 {
				last = n
			}
			d, ok := m.Data.(*clientv1.Delta)
			return ok && d.GetAckSeq() >= seq
		})
		return last
	}
	// attack swings at the slime during the last input sent, as the client presses, and returns the tick its blow lands, at the attack's start, and the energy told after it.
	attack := func() (hit uint64, energy int) {
		t.Helper()
		p.send(wsconn.Message{Seq: p.seq, Data: &clientv1.Attack{}})
		energy = -1
		until("blow and the energy it gave", func(m wsconn.Message) bool {
			if d, ok := m.Data.(*clientv1.Damage); ok && d.GetSource() == "p:"+id {
				hit = m.Tick
			}
			if n := told(m); n >= 0 {
				energy = n
			}
			return hit > 0 && energy >= 0
		})
		return hit, energy
	}

	until("join's energy", func(m wsconn.Message) bool { return told(m) == 0 }) // the first Delta after a join tells it
	p.face(true)

	p.send(wsconn.Message{Seq: p.seq, ReqID: "broke", Data: &clientv1.UseSkill{Skill: "strike"}})
	var refusal *clientv1.Response
	until("answer to Strike", func(m wsconn.Message) bool {
		refusal, _ = m.Data.(*clientv1.Response)
		return refusal != nil && m.ReqID == "broke"
	})
	if refusal.GetOk() || refusal.GetCode() != code("NOT_ENOUGH_ENERGY") {
		t.Fatalf("Strike with no energy: %+v, want NOT_ENOUGH_ENERGY", refusal)
	}
	if e := ack(); e >= 0 {
		t.Fatalf("energy told %d after the refusal, want nothing spent and so none told", e)
	}

	hit, e := attack()
	if e != 2 {
		t.Fatalf("an attack on the slime left %d energy, want 2", e)
	}
	past(hit + 21) // the Beginner's 21-tick attack

	p.send(wsconn.Message{Seq: p.seq, ReqID: "paid", Data: &clientv1.UseSkill{Skill: "strike"}})
	var answer *clientv1.Response
	var used uint64
	e = -1
	until("Strike's answer and its use", func(m wsconn.Message) bool {
		switch d := m.Data.(type) {
		case *clientv1.Response:
			if m.ReqID == "paid" {
				answer = d
			}
		case *clientv1.SkillUsed:
			if d.GetCaster() == id {
				used = m.Tick
			}
		}
		if n := told(m); n >= 0 {
			e = n
		}
		return answer != nil && (!answer.GetOk() || used > 0)
	})
	if !answer.GetOk() || e >= 0 {
		t.Fatalf("Strike with 2 energy: %+v, energy told %d; want it used, and nothing told before the input after its press", answer, e)
	}
	if e := ack(); e != 0 { // the next input, as a client sends one every tick
		t.Fatalf("energy told %d with the input after Strike's press, want 0 left", e)
	}
	past(used + 30) // Strike's 30 ticks

	if _, e := attack(); e != 2 {
		t.Fatalf("a second attack left %d energy, want 2", e)
	}
	_, w := s.welcome(t, "spark", id)
	if e := w.GetCharacter().GetEnergy(); e != 2 {
		t.Fatalf("welcomed back with %d energy, want the 2 it left with", e)
	}
}

// Animation cancel §15: an attack's gain reaches the client in a Delta ahead of the attack's own AttackUsed, in the same tick, so a skill short of energy waits for that AttackUsed, then goes at the attack's cancel point and lands.
func TestASkillWaitingForItsAttacksGainCutsIt(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, gameData(t, append(stillSlimes(), loneSlime())...))
	slime := groundSlime(t, s)
	token, id := s.createCharacter(t, "count", "Count", "beginner")
	if _, err := s.db.Exec(context.Background(), `INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 1, now(), now())`, id); err != nil {
		t.Fatal(err)
	}
	s.place(t, id, "forest", slime.GetX()-40, slime.GetY(), 135, 0) // in reach of a 48 px swing, its box clear of the slime's
	p, _ := s.join(t, token, id)
	p.face(true)
	p.drain(300 * time.Millisecond) // past the join's Delta telling no energy

	// until reads until pick says done; 15 s only guards a hang, as a slow runner's ticks lag the wall clock.
	until := func(what string, pick func(wsconn.Message) bool) {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); {
			if m, ok := p.read(time.Until(deadline)); ok && pick(m) {
				return
			}
		}
		t.Fatalf("no %s within 15 s", what)
	}

	p.send(wsconn.Message{Seq: p.nextSeq(), Data: &clientv1.Attack{}})
	var gained, started uint64
	until("the attack's AttackUsed", func(m wsconn.Message) bool {
		switch d := m.Data.(type) {
		case *clientv1.Delta:
			if d.Energy != nil && d.GetEnergy() == 2 && gained == 0 {
				gained = m.Tick
			}
		case *clientv1.AttackUsed:
			started = m.Tick
			return d.GetAttacker() == id
		}
		return false
	})
	if gained == 0 || gained != started {
		t.Fatalf("ahead of the AttackUsed of tick %d, the gain was told at tick %d (0 for not yet); want it told first, in the same tick", started, gained)
	}

	p.send(wsconn.Message{ReqID: "counted", Data: &clientv1.UseSkill{Skill: "strike"}})
	var answer *clientv1.Response
	var used, landed uint64
	until("Strike's answer, its use and its blow", func(m wsconn.Message) bool {
		switch d := m.Data.(type) {
		case *clientv1.Response:
			if m.ReqID == "counted" {
				answer = d
			}
		case *clientv1.SkillUsed:
			if d.GetCaster() == id {
				used = m.Tick
			}
		case *clientv1.Damage:
			if d.GetSource() == "p:"+id && used > 0 {
				landed = m.Tick
			}
		}
		return answer != nil && (!answer.GetOk() || landed > 0)
	})
	// The world uses a skill up to castSlack's 3 ticks before the attack's cancel point, 6 of its 21 ticks in; a slow runner may send it later, but before the end it still cuts the attack.
	cancel, end := started+6, started+21
	if !answer.GetOk() || used < cancel-3 || used >= end || landed != used {
		t.Fatalf("Strike %+v used at tick %d, landing at %d; want it from %d, castSlack before the attack's cancel point, and before its end at %d, landing then", answer, used, landed, cancel-3, end)
	}
}
