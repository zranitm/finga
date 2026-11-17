package e2e_test

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// strikeChain has the Beginner's Strike key offer Throw within 2 s of Strike's hit, in place of Lunge at a far lock.
var strikeChain = e2etest.DataEdit{File: "classes.yaml", Old: "      - {root: strike, follow: [{skill: lunge, when: {far: 3}}]}\n", New: "      - {root: strike, follow: [{skill: throw, when: {after: 2s}}]}\n"}

// chainer is a character of class at level 20 with a point in each skill and full energy.
func chainer(t *testing.T, s *e2etest.Stack, name, class string, skills ...string) (string, string) {
	t.Helper()
	token, id := s.CreateCharacter(t, name, name, class)
	if _, err := s.DB.Exec(context.Background(), `UPDATE characters SET energy = 10, level = 20 WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	for _, sk := range skills {
		if _, err := s.DB.Exec(context.Background(), `INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, $2, 1, now(), now())`, id, sk); err != nil {
			t.Fatal(err)
		}
	}
	return token, id
}

// chained presses skill from root's key during a fresh input, aimed along aim at the lock, and waits up to 10 s for the world's answer.
func chained(t *testing.T, l *e2etest.Looter, req, skill, root string, lock uint32, aim float32, way *clientv1.SkillWay) *clientv1.Response {
	t.Helper()
	if err := l.Input(&clientv1.Input{}); err != nil {
		t.Fatal(err)
	}
	l.Send(wsconn.Message{ReqID: req, Seq: l.Seq, Data: &clientv1.UseSkill{Skill: skill, ChainRoot: root, Target: lock, Aim: aim, Way: way}})
	var got *clientv1.Response
	l.Pump(10*time.Second, func(m wsconn.Message) bool {
		if r, ok := m.Data.(*clientv1.Response); ok && m.ReqID == req {
			got = r
		}
		return got != nil
	})
	if got == nil {
		t.Fatalf("%s: no answer in 10 s", req)
	}
	return got
}

// C18: chain_root crosses the wire: Throw from Strike's key is refused with SKILL_CHAIN_NOT_READY before Strike, and goes a few inputs after it, counted in inputs, so the runner's speed doesn't matter.
func TestAFollowUpGoesFromItsRootsKeyAfterTheRoot(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, strikeChain))
	token, id := s.CreateCharacter(t, "chainer", "Chainer", "beginner")
	for _, q := range []string{
		`INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 1, now(), now()), ($1, 'throw', 1, now(), now())`,
		`UPDATE characters SET energy = 10 WHERE id = $1`,
	} {
		if _, err := s.DB.Exec(context.Background(), q, id); err != nil {
			t.Fatal(err)
		}
	}
	s.Place(t, id, "forest", e2etest.OutOfSight, 134, 0)
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Pump(300*time.Millisecond, nil)
	use := func(req, skill string) *clientv1.Response {
		t.Helper()
		return chained(t, l, req, skill, "strike", 0, 0, nil)
	}
	if r := use("early", "throw"); r.GetOk() || r.GetCode() != 330034 {
		t.Fatalf("Throw from Strike's key before Strike: %+v, want SKILL_CHAIN_NOT_READY", r)
	}
	if r := use("root", "strike"); !r.GetOk() {
		t.Fatalf("Strike refused with %d", r.GetCode())
	}
	for range 3 {
		if err := l.Input(&clientv1.Input{}); err != nil {
			t.Fatal(err)
		}
	}
	if r := use("follow", "throw"); !r.GetOk() {
		t.Fatalf("Throw from Strike's key four inputs after it refused with %d", r.GetCode())
	}
}

// stunnableSlime is the forest's one slime in the slimes' circle, standing still and outlasting any fight with no super armor, so a stun takes; a hit lands but for one in a million.
// Charge's hit comes at 0.9, so its dash is over by then on any runner whose inputs lag the world's ticks.
func stunnableSlime() []e2etest.DataEdit {
	return []e2etest.DataEdit{
		{File: "skills.yaml", Old: "    hit: 0.3\n    cancel: 0.45\n    power: {base: 1.8, per_point: 0.05}\n    targets: 1\n    move: {kind: dash, distance: 7,", New: "    hit: 0.9\n    cancel: 0.9\n    power: {base: 1.8, per_point: 0.05}\n    targets: 1\n    move: {kind: dash, distance: 7,"},
		{File: "mobs.yaml", Old: "    name: Green Slime\n    level: 1\n    hp: 34\n", New: "    name: Green Slime\n    level: 1\n    hp: 1000000\n"},
		{File: "mobs.yaml", Old: greenSlimeBody, New: "    exp: 8\n    speed: 0\n    radius: 0.5\n    height: 0.875\n    behavior: patrol\n    attacks: [{use: bite, reach: 0.03}]\n"},
		{File: "stats.yaml", Old: "hit: {top: 0.94, k: 0.175,", New: "hit: {top: 1, k: 0.000001,"},
		{File: "stats.yaml", Old: "hit_chance: [0.20, 0.99]", New: "hit_chance: [0.20, 1]"},
		loneSlime(),
	}
}

// C18: a Warrior's Strike key is Charge at a lock past 3 m and Ground Slam on the lock Charge stunned, as the client offers them: the world takes each from Strike's key, judged at the press, and refuses Ground Slam there before the stun.
func TestAWarriorsStrikeKeyChargesThenSlams(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, stunnableSlime()...))
	slime := groundSlime(t, s)
	token, id := chainer(t, s, "Charger", "warrior", "strike", "charge", "ground_slam")
	s.Place(t, id, "forest", beside(slime, -6), 500, 0)
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Pump(300*time.Millisecond, nil)
	lock, east := slime.GetId(), float32(math.Pi/2)
	if r := chained(t, l, "early", "ground_slam", "strike", lock, east, nil); r.GetOk() || r.GetCode() != 330034 {
		t.Fatalf("Ground Slam from Strike's key on a slime not stunned: %+v, want SKILL_CHAIN_NOT_READY", r)
	}
	toContact := &clientv1.SkillWay{X: 1, Length: 6 - 0.5 - 0.375} // east to the slime's side, as the client sends a dash at the lock
	if r := chained(t, l, "charge", "charge", "strike", lock, east, toContact); !r.GetOk() {
		t.Fatalf("Charge from Strike's key at the slime 6 m off refused with %d", r.GetCode())
	}
	stunned := false // an input a tick, as a client sends them, carries the dash to the slime
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && !stunned; {
		if err := l.Input(&clientv1.Input{}); err != nil {
			t.Fatal(err)
		}
		l.Pump(50*time.Millisecond, func(m wsconn.Message) bool {
			if d, ok := m.Data.(*clientv1.Delta); ok {
				for _, mb := range d.GetMobs() {
					stunned = stunned || mb.GetId() == lock && slices.ContainsFunc(mb.GetStatuses(), func(st *clientv1.StatusView) bool {
						return st.GetId() == "stun" && uint64(st.GetEnds()) > m.Tick
					})
				}
			}
			return stunned
		})
	}
	if !stunned {
		t.Fatal("no frame of the slime stunned within 10 s of Charge")
	}
	if r := chained(t, l, "slam", "ground_slam", "strike", lock, east, nil); !r.GetOk() {
		t.Fatalf("Ground Slam from Strike's key on the stunned slime refused with %d", r.GetCode())
	}
}

// longDisengage has Disengage dodge for 10 s, so a slime's bite surely passes through it.
var longDisengage = e2etest.DataEdit{File: "skills.yaml", Old: "    move: {kind: leap, distance: 5, time: 300ms, toward: back}\n    dodge: 400ms\n", New: "    move: {kind: leap, distance: 5, time: 300ms, toward: back}\n    dodge: 10s\n"}

// C18: a Bowman's Disengage key is Power Shot right after a dodge: the world refuses it from that key before any, and takes it once the slime's bite passed through Disengage's dodge, counted from the dodge's frame.
func TestABowmansDisengageKeyShootsAfterADodge(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, append(biters(true), loneSlime(), longDisengage)...))
	slime := groundSlime(t, s)
	token, id := chainer(t, s, "Counter", "bowman", "disengage", "power_shot")
	s.Place(t, id, "forest", beside(slime, -1), 300, 0) // in its bite's reach
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Pump(300*time.Millisecond, nil)
	if r := chained(t, l, "early", "power_shot", "disengage", 0, 0, nil); r.GetOk() || r.GetCode() != 330034 {
		t.Fatalf("Power Shot from Disengage's key before any dodge: %+v, want SKILL_CHAIN_NOT_READY", r)
	}
	// Disengage leaps in place, so the slime keeps biting, and dodges.
	if r := chained(t, l, "disengage", "disengage", "disengage", 0, 0, &clientv1.SkillWay{X: -1}); !r.GetOk() {
		t.Fatalf("Disengage refused with %d", r.GetCode())
	}
	dodged := false
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && !dodged; {
		if err := l.Input(&clientv1.Input{}); err != nil {
			t.Fatal(err)
		}
		l.Pump(50*time.Millisecond, func(m wsconn.Message) bool {
			if d, ok := m.Data.(*clientv1.Damage); ok && d.GetTarget() == "p:"+id && d.GetDodge() {
				dodged = true
			}
			return dodged
		})
	}
	if !dodged {
		t.Fatal("no bite of the slime's passed through the dodge within 10 s")
	}
	if r := chained(t, l, "counter", "power_shot", "disengage", 0, 0, nil); !r.GetOk() {
		t.Fatalf("Power Shot from Disengage's key right after the dodge refused with %d", r.GetCode())
	}
}
