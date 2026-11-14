package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// stillSlimes stand still in super armor, outlast any fight and bite 1 px out, so each hit shows where the slime was found.
func stillSlimes() []dataEdit {
	return []dataEdit{
		{"mobs.yaml", "    name: Green Slime\n    level: 1\n    hp: 34\n", "    name: Green Slime\n    level: 1\n    hp: 1000000\n"},
		{"mobs.yaml", "    exp: 8\n    speed: 32\n    width: 32\n    height: 28\n    behavior: patrol\n    attacks: [{use: hop}]\n", "    exp: 8\n    speed: 0\n    width: 32\n    height: 28\n    behavior: patrol\n    super_armor: true\n    attacks: [{use: bite, reach: 1}]\n"},
	}
}

// C9: an attack, Strike and an attack again, chained, land sooner than the same three one at a time, and no sooner than the chain allows.
func TestAChainLandsSoonerThanOneAtATime(t *testing.T) {
	t.Parallel()
	// Strike cools down as it ends, so the test needn't wait out its 4.5 s.
	quickStrike := dataEdit{"skills.yaml", "width: 64, height: 56}\n    energy: 2\n    cooldown: 4500ms\n", "width: 64, height: 56}\n    energy: 2\n    cooldown: 1500ms\n"}
	s := newStackOver(t, gameData(t, append(stillSlimes(), quickStrike, loneSlime())...))
	// A scout starts the forest and finds its one slime on the ground, which stays put: a second beside it would bite the player through it.
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

	attack := func() { p.send(wsconn.Message{Seq: p.nextSeq(), Data: &clientv1.Attack{}}) }
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
