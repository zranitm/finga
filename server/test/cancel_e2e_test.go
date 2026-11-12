package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// stillSlimes has the forest's green slimes stand still and outlast any fight, so every action's hit shows where the slime was found.
func stillSlimes() []dataEdit {
	return []dataEdit{{"mobs.yaml", "    name: Green Slime\n    level: 1\n    hp: 34\n", "    name: Green Slime\n    level: 1\n    hp: 1000000\n"}, {"mobs.yaml", "    exp: 8\n    speed: 32\n", "    exp: 8\n    speed: 0\n"}}
}

// C9: an attack, Strike and an attack again, chained, land sooner than the same three one at a time, and no sooner than the chain allows.
func TestAChainLandsSoonerThanOneAtATime(t *testing.T) {
	t.Parallel()
	// Strike cools down as it ends, so the test needn't wait out its 4.5 s.
	quickStrike := dataEdit{"skills.yaml", "width: 64, height: 56}\n    mp: {base: 4, per_point: 0.2}\n    cooldown: 4500ms\n", "width: 64, height: 56}\n    mp: {base: 4, per_point: 0.2}\n    cooldown: 1500ms\n"}
	s := newStackOver(t, gameData(t, append(stillSlimes(), quickStrike)...))
	// A scout starts the forest and finds a slime on the ground, which stays put.
	scoutToken, scoutID := s.createCharacter(t, "scout", "Scout", "beginner")
	s.place(t, scoutID, "forest", 100, 608, 135, 0)
	_, snap := s.join(t, scoutToken, scoutID)
	var slime *clientv1.MobView
	for _, m := range snap.GetMobs() {
		if m.GetType() == "green_slime" && (slime == nil || m.GetY() > slime.GetY()) {
			slime = m
		}
	}
	if slime == nil {
		t.Fatal("no green slime in the forest")
	}
	token, id := s.createCharacter(t, "chain", "Chain", "beginner")
	if _, err := s.db.Exec(context.Background(), `INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 1, now(), now())`, id); err != nil {
		t.Fatal(err)
	}
	s.place(t, id, "forest", slime.GetX()-40, slime.GetY(), 135, 0) // in reach of a 48 px swing, its box clear of the slime's
	p, _ := s.join(t, token, id)
	p.face(true)
	p.drain(300 * time.Millisecond)

	seq := uint32(1000)
	attack := func() {
		seq++
		p.send(wsconn.Message{Seq: seq, Data: &clientv1.Attack{}})
	}
	strike := func(req string) { p.send(wsconn.Message{ReqID: req, Data: &clientv1.UseSkill{Skill: "strike"}}) }
	// hit is the tick of the player's next blow, a MISS included.
	hit := func() uint64 {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			m, ok := p.read(100 * time.Millisecond)
			if d, isDamage := m.Data.(*clientv1.Damage); ok && isDamage && d.GetSource() == "p:"+id {
				return m.Tick
			}
			if r, isResponse := m.Data.(*clientv1.Response); ok && isResponse && !r.GetOk() {
				t.Fatalf("%s refused: %+v", m.ReqID, r)
			}
		}
		t.Fatal("no blow in 5 s")
		return 0
	}

	// One at a time: each sent once the last has ended, the Beginner's attack 1050 ms and Strike 1500 ms.
	attack()
	first := hit()
	time.Sleep(1200 * time.Millisecond)
	strike("one")
	hit()
	time.Sleep(1600 * time.Millisecond)
	attack()
	apart := hit() - first

	time.Sleep(1200 * time.Millisecond) // the attack ends, and Strike's cooldown with it
	// Chained: Strike and the attack sent as the first attack lands; the world holds each to the chain.
	attack()
	first = hit()
	strike("chained")
	attack()
	hit()
	chained := hit() - first
	const perfect = 17 + 25 // the attack's 21 ticks cut short at 17, Strike's 30 at 25
	if chained != perfect || chained >= apart {
		t.Fatalf("chained, the third blow came %d ticks after the first, one at a time %d; want %d, the chain's own pace", chained, apart, perfect)
	}
}
