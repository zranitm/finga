package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// strikeChain gives the Beginner a chain: Strike's key offers Throw within 2 s of Strike's hit.
var strikeChain = e2etest.DataEdit{File: "classes.yaml", Old: "    skills: [strike, throw, spark, second_wind, lunge, roll_back]\n", New: `    chains:
      - root: strike
        follow:
          - {skill: throw, when: {after: 2s}}
    skills: [strike, throw, spark, second_wind, lunge, roll_back]
`}

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
		if err := l.Input(&clientv1.Input{}); err != nil {
			t.Fatal(err)
		}
		l.Send(wsconn.Message{ReqID: req, Seq: l.Seq, Data: &clientv1.UseSkill{Skill: skill, ChainRoot: "strike"}})
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
