package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// stillSlimes stand still in super armor, outlast any fight and bite 0.03 m out, so each hit shows where the slime was found.
func stillSlimes() []e2etest.DataEdit {
	return []e2etest.DataEdit{
		{File: "mobs.yaml", Old: "    name: Green Slime\n    level: 1\n    hp: 34\n", New: "    name: Green Slime\n    level: 1\n    hp: 1000000\n"},
		{File: "mobs.yaml", Old: greenSlimeBody, New: "    exp: 8\n    speed: 0\n    radius: 0.5\n    height: 0.875\n    behavior: patrol\n    super_armor: true\n    attacks: [{use: bite, reach: 0.03}]\n"},
	}
}

// C9: an attack, Strike and an attack again, chained, land sooner than the same three one at a time, and no sooner than the chain allows.
func TestAChainLandsSoonerThanOneAtATime(t *testing.T) {
	t.Parallel()
	// Strike cools down as it ends, so the test needn't wait out its 4.5 s.
	quickStrike := e2etest.DataEdit{File: "skills.yaml", Old: "radius: 2.375, angle: 120, height: 1.75}\n    energy: 2\n    cooldown: 4500ms\n", New: "radius: 2.375, angle: 120, height: 1.75}\n    energy: 2\n    cooldown: 1500ms\n"}
	s := newStackOver(t, e2etest.GameData(t, append(stillSlimes(), quickStrike, loneSlime())...))
	// A scout starts the forest and finds its one slime in the slimes' circle, which stays put: a second beside it would bite the player through it.
	slime := groundSlime(t, s)
	token, id := s.CreateCharacter(t, "chain", "Chain", "beginner")
	if _, err := s.DB.Exec(context.Background(), `INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 1, now(), now())`, id); err != nil {
		t.Fatal(err)
	}
	s.Place(t, id, "forest", beside(slime, -1.25), 135, 0) // in reach of a 1.875 m swing, its body clear of the slime's
	p, _ := s.Join(t, token, id)
	p.Drain(300 * time.Millisecond)

	attack := func() { p.Send(wsconn.Message{Seq: p.NextSeq(), Data: &clientv1.Attack{Aim: east}}) }
	strike := func(req string) {
		p.Send(wsconn.Message{ReqID: req, Data: &clientv1.UseSkill{Skill: "strike", Aim: east}})
	}
	// hit is the tick of the player's next blow, a MISS included.
	hit := func() uint64 {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
			m, ok := p.Read(100 * time.Millisecond)
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

	// One at a time: each sent once the last has ended, the Beginner's attack 1050 ms and Strike 1000 ms.
	attack()
	first := hit()
	time.Sleep(1200 * time.Millisecond)
	strike("one")
	hit()
	time.Sleep(1150 * time.Millisecond)
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
	const perfect = 6 + 9 // the attack's 21 ticks cut short at 6, Strike's 20 at 9, each 450 ms in at most
	if chained != perfect || chained >= apart {
		t.Fatalf("chained, the third blow came %d ticks after the first, one at a time %d; want %d, the chain's own pace", chained, apart, perfect)
	}
}
