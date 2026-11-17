package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// hopBack teaches the Beginner Hop Back: a leap of up to 4 m back over 300 ms, dodging for 10 s from its start.
var hopBack = []e2etest.DataEdit{
	{File: "skills.yaml", Old: "skills:\n  - id: strike\n", New: `skills:
  - id: hop_back
    icon: lorc/dodging
    kind: active
    effect: move
    energy: 2
    cooldown: 1s
    length: 600ms
    move: {kind: leap, distance: 4, time: 300ms, toward: back}
    dodge: 10s
    stages:
      - {at: 0, name: Hop Back, description: "Hops back, dodging."}
  - id: strike
`},
	{File: "classes.yaml", Old: "skills: [strike, throw, spark, second_wind]", New: "skills: [strike, throw, spark, second_wind, hop_back]"},
}

// C17: a moving skill's way crosses the wire, its leap shows in the player's frames from the input after the press, and the slimes' attacks pass through its dodge with no damage.
// The dodge counts from the tick the skill is used, so a bite before it, on a slow runner, is left out.
func TestASlimesAttackPassesThroughADodge(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, hopBack...))
	token, id := s.CreateCharacter(t, "dodger", "Dodger", "beginner")
	for _, q := range []string{
		`INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'hop_back', 1, now(), now())`,
		`UPDATE characters SET energy = 10 WHERE id = $1`,
	} {
		if _, err := s.DB.Exec(context.Background(), q, id); err != nil {
			t.Fatal(err)
		}
	}
	s.Place(t, id, "forest", e2etest.SlimeField, 134, 0)
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Pump(300*time.Millisecond, nil)

	// The leap goes 1 m south, out of the slimes' circle no farther than a step.
	if err := l.Input(&clientv1.Input{}); err != nil {
		t.Fatal(err)
	}
	l.Send(wsconn.Message{ReqID: "hop", Seq: l.Seq, Data: &clientv1.UseSkill{Skill: "hop_back", Way: &clientv1.SkillWay{Z: -1, Length: 1}}})
	answered, leapt, dodging := false, false, false
	var used uint64
	var hp int32 // as the skill is used: its tick's frame comes before its events
	var dodged []*clientv1.Damage
	for deadline := time.Now().Add(9 * time.Second); time.Now().Before(deadline) && len(dodged) < 2; {
		if err := l.Input(&clientv1.Input{}); err != nil {
			t.Fatal(err)
		}
		l.Pump(50*time.Millisecond, func(m wsconn.Message) bool {
			switch d := m.Data.(type) {
			case *clientv1.Response:
				if m.ReqID == "hop" {
					if !d.GetOk() {
						t.Fatalf("Hop Back refused with %d", d.GetCode())
					}
					answered = true
				}
			case *clientv1.SkillUsed:
				if used == 0 {
					used, hp = m.Tick, l.HP
				}
			case *clientv1.Delta:
				for _, v := range d.GetPlayers() {
					if v.GetId() == id {
						leapt = leapt || v.GetMove().GetLeft() > 0 && v.GetMove().GetVz() < 0
						dodging = dodging || v.GetDodging()
					}
				}
			case *clientv1.Damage:
				if d.GetTarget() == "p:"+id && used > 0 && m.Tick >= used {
					if !d.GetDodge() || d.GetAmount() != 0 {
						t.Fatalf("a slime's attack in the dodge %+v, want it passing through", d)
					}
					dodged = append(dodged, d)
				}
			}
			return false
		})
	}
	if !answered || !leapt || !dodging || len(dodged) < 2 {
		t.Fatalf("answered %v, a frame of the leap southward %v, of the dodge %v, %d attacks dodged; want all, and two", answered, leapt, dodging, len(dodged))
	}
	if l.HP != hp {
		t.Fatalf("HP %d after the dodged attacks, want %d", l.HP, hp)
	}
}

// C17: a skill pressed mid-swing waits for the attack's cancel point, the gateway holding the inputs after the press until it's answered, so its move starts from the input after the press, as the client marks it.
// A slow runner can bring the press to the world past the cancel point, cast at once: its move starts from the same input, so the test holds either way and says which it saw.
func TestAMoveStartsFromTheInputAfterAPressMidSwing(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, hopBack...))
	token, id := s.CreateCharacter(t, "swinger", "Swinger", "beginner")
	for _, q := range []string{
		`INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'hop_back', 1, now(), now())`,
		`UPDATE characters SET energy = 10 WHERE id = $1`,
	} {
		if _, err := s.DB.Exec(context.Background(), q, id); err != nil {
			t.Fatal(err)
		}
	}
	s.Place(t, id, "forest", e2etest.OutOfSight, 134, 0)
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Pump(300*time.Millisecond, nil)

	if err := l.Input(&clientv1.Input{}); err != nil {
		t.Fatal(err)
	}
	l.Send(wsconn.Message{Data: &clientv1.Attack{}})
	for range 2 { // the swing under way
		if err := l.Input(&clientv1.Input{}); err != nil {
			t.Fatal(err)
		}
		l.Pump(50*time.Millisecond, nil)
	}
	press := l.Seq // pressed two inputs into the swing, before its cancel point
	l.Send(wsconn.Message{ReqID: "hop", Seq: press, Data: &clientv1.UseSkill{Skill: "hop_back", Way: &clientv1.SkillWay{Z: -1, Length: 1}}})
	var pressed, used uint64
	var answered bool
	var after *clientv1.PlayerView // the player in the first frame acking an input past the press
	var afterAck uint32
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && (after == nil || !answered); {
		if err := l.Input(&clientv1.Input{}); err != nil {
			t.Fatal(err)
		}
		l.Pump(50*time.Millisecond, func(m wsconn.Message) bool {
			switch d := m.Data.(type) {
			case *clientv1.Response:
				if m.ReqID == "hop" {
					if !d.GetOk() {
						t.Fatalf("Hop Back refused with %d", d.GetCode())
					}
					answered = true
				}
			case *clientv1.SkillUsed:
				used = m.Tick
			case *clientv1.Delta:
				if d.GetAckSeq() == press && pressed == 0 {
					pressed = m.Tick
				}
				for _, v := range d.GetPlayers() {
					if v.GetId() == id && d.GetAckSeq() > press && after == nil {
						after, afterAck = v, d.GetAckSeq()
					}
				}
			}
			return false
		})
	}
	if !answered || after == nil {
		t.Fatalf("answered %v, a frame past the press %v, within 10 s", answered, after != nil)
	}
	if m := after.GetMove(); m.GetLeft() != 15 || m.GetVz() >= 0 {
		t.Fatalf("the first frame acking past the press's input %d (it acks %d) has move %+v; want the leap 3 of its 18 sub-steps in", press, afterAck, m)
	}
	if used > pressed {
		t.Logf("Hop Back waited: the press's input acked on tick %d, the skill used on %d", pressed, used)
	} else {
		t.Logf("the press reached the world past the attack's cancel point: input acked on tick %d, the skill used on %d", pressed, used)
	}
}
