package e2e_test

import (
	"context"
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// slamming slimes use the field bosses' slam, its 1 s stun as mobs.yaml gives it.
var slamming = []e2etest.DataEdit{
	{File: "mobs.yaml", Old: "    attacks: [{use: hop}]\n    look: {model: green_blob}\n", New: "    attacks: [{use: slam}]\n    look: {model: green_blob}\n"}, // the green slime's
}

// C19: a slam stuns a player, as every field boss's does: their frames show the stun to its end tick and hold them where they stand while they press a key,
// a skill pressed meanwhile is refused CANT_ACT over the wire, and once it ends their frames show the 2 s hard immunity. Judged by the frames' ticks and acks.
func TestASlamStunsAPlayer(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, slamming...))
	token, id := s.CreateCharacter(t, "stunned", "Stunned", "beginner")
	for _, q := range []string{
		`INSERT INTO character_skills (character_id, skill_id, points, created_at, updated_at) VALUES ($1, 'strike', 1, now(), now())`,
		`UPDATE characters SET energy = 10 WHERE id = $1`,
	} {
		if _, err := s.DB.Exec(context.Background(), q, id); err != nil {
			t.Fatal(err)
		}
	}
	s.Place(t, id, "forest", e2etest.SlimeField, 134, 0)
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Pump(300*time.Millisecond, nil)

	var hit, ends uint64 // the slam's tick, and its stun's end
	var heldAt *clientv1.PlayerView
	held, refused, immune := 0, false, false
	sent := false
	for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline) && !(refused && immune && held >= 3); {
		in := &clientv1.Input{}
		if ends > 0 {
			in.Right = true // pressing east all through the stun
		}
		if err := l.Input(in); err != nil {
			t.Fatal(err)
		}
		l.Pump(50*time.Millisecond, func(m wsconn.Message) bool {
			switch d := m.Data.(type) {
			case *clientv1.Damage:
				if d.GetTarget() == "p:"+id && !d.GetMiss() && hit == 0 {
					hit, ends = m.Tick, m.Tick+21 // from the tick after the slam, for its 20
				}
			case *clientv1.Response:
				if m.ReqID == "strike" {
					refused = refused || !d.GetOk() && d.GetCode() == 330032
				}
			case *clientv1.Delta:
				for _, v := range d.GetPlayers() {
					if v.GetId() != id || hit == 0 {
						continue
					}
					stunned := slices.ContainsFunc(v.GetStatuses(), func(s *clientv1.StatusView) bool { return s.GetId() == "stun" && uint64(s.GetEnds()) == ends })
					switch {
					case v.GetHeld() > 0 && stunned && uint64(m.Tick) < ends:
						if heldAt == nil {
							heldAt = v
						} else if v.GetX() != heldAt.GetX() || v.GetZ() != heldAt.GetZ() {
							t.Fatalf("held at tick %d at (%v, %v), first held at (%v, %v); want them where they stood", m.Tick, v.GetX(), v.GetZ(), heldAt.GetX(), heldAt.GetZ())
						} else {
							held++
						}
						if !sent {
							sent = true
							l.Send(wsconn.Message{ReqID: "strike", Seq: l.Seq, Data: &clientv1.UseSkill{Skill: "strike"}})
						}
					case m.Tick >= ends && len(v.GetStatuses()) == 0:
						immune = immune || uint64(v.GetImmune()) == ends+40
					}
				}
			}
			return false
		})
	}
	if hit == 0 || held < 3 || !refused || !immune {
		t.Fatalf("slammed at tick %d; %d frames held in place, a skill refused CANT_ACT %v, the immunity to tick %d shown %v; want a slam, 3 frames, and both", hit, held, refused, ends+40, immune)
	}
}
