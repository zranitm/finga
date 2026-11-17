package e2e_test

import (
	"context"
	"fmt"
	"math"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// meteor teaches the Beginner a test Meteor: an area of 2 m at the lock within 14 m.
var meteor = []e2etest.DataEdit{
	{File: "skills.yaml", Old: "skills:\n  - id: strike\n", New: `skills:
  - id: meteor
    icon: lorc/fire-ring
    kind: active
    effect: damage
    damage: magic
    shape: {kind: area, radius: 2, at: lock, reach: 14}
    energy: 1
    cooldown: 1s
    length: 1s
    power: {base: 0.1}
    targets: 6
    stages:
      - {at: 0, name: Meteor, description: "A meteor on the lock."}
  - id: strike
`},
	{File: "classes.yaml", Old: "skills: [strike, throw, spark, second_wind]", New: "skills: [strike, throw, spark, second_wind, meteor]"},
}

// C17: an area at the lock falls on the locked slime 10 m off; with no lock it falls 14 m along the aim, past the slime.
func TestAnAreaAtTheLockFallsOnTheLockedSlime(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, append(append(stillSlimes(), loneSlime()), meteor...)...))
	slime := groundSlime(t, s)
	token, id := s.CreateCharacter(t, "caster", "Caster", "beginner")
	for _, q := range []string{
		`INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'meteor', 1, now(), now())`,
		`UPDATE characters SET energy = 10 WHERE id = $1`,
	} {
		if _, err := s.DB.Exec(context.Background(), q, id); err != nil {
			t.Fatal(err)
		}
	}
	s.Place(t, id, "forest", beside(slime, -10), 134, 0)
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Pump(300*time.Millisecond, nil)
	east := float32(math.Pi / 2)
	struck := func(req string, target uint32) bool {
		t.Helper()
		l.Send(wsconn.Message{ReqID: req, Seq: l.Seq, Data: &clientv1.UseSkill{Skill: "meteor", Aim: east, Target: target}})
		hit := false
		l.Pump(1200*time.Millisecond, func(m wsconn.Message) bool {
			switch d := m.Data.(type) {
			case *clientv1.Response:
				if m.ReqID == req && !d.GetOk() {
					t.Fatalf("%s refused with %d", req, d.GetCode())
				}
			case *clientv1.Damage:
				hit = hit || d.GetSource() == "p:"+id && d.GetTarget() == fmt.Sprintf("m:%d", slime.GetId())
			}
			return false
		})
		return hit
	}
	if struck("unlocked", 0) {
		t.Fatal("struck the slime with no lock; want the area 14 m along the aim, 4 m past it")
	}
	if !struck("locked", slime.GetId()) {
		t.Fatal("missed the locked slime 10 m off; want the area on it")
	}
}
