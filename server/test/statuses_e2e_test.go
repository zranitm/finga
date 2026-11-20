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

// slamming slimes use the field bosses' slam, its 1 s stun as mobs.yaml gives it; only a boss's is unblockable (C21), so theirs isn't.
var slamming = []e2etest.DataEdit{
	{File: "mobs.yaml", Old: "    attacks: [{use: hop}]\n    look: {model: green_blob}\n", New: "    attacks: [{use: slam, unblockable: false}]\n    look: {model: green_blob}\n"}, // the green slime's
}

// C19: a slam stuns a player, as every field boss's does: their frames show the stun to its end tick and hold them where they stand while they press a key,
// a skill pressed meanwhile is refused CANT_ACT over the wire, and once it ends their frames show no status. Judged by the frames' ticks and acks.
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
	l.Await(300*time.Millisecond, nil)

	var hit, ends uint64 // the slam's tick, and its stun's end
	var heldAt *clientv1.PlayerView
	held, refused, freed := 0, false, false
	sent := false
	for game := l.Game(15 * time.Second); game.Left() && (!refused || !freed || held < 3); {
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
					case v.GetHeld() > 0 && stunned && m.Tick < ends:
						switch {
						case heldAt == nil:
							heldAt = v
						case v.GetX() != heldAt.GetX() || v.GetZ() != heldAt.GetZ():
							t.Fatalf("held at tick %d at (%v, %v), first held at (%v, %v); want them where they stood", m.Tick, v.GetX(), v.GetZ(), heldAt.GetX(), heldAt.GetZ())
						default:
							held++
						}
						if !sent {
							sent = true
							l.Send(wsconn.Message{ReqID: "strike", Seq: l.Seq, Data: &clientv1.UseSkill{Skill: "strike"}})
						}
					case m.Tick >= ends && len(v.GetStatuses()) == 0:
						freed = true
					}
				}
			}
			return false
		})
	}
	if hit == 0 || held < 3 || !refused || !freed {
		t.Fatalf("slammed at tick %d; %d frames held in place, a skill refused CANT_ACT %v, freed by tick %d %v; want a slam, 3 frames, and both", hit, held, refused, ends, freed)
	}
}

// C19 §13: a slammed Beginner presses Roll Back's key, which offers Break Free while held, granted with no points: the frame acking the input after the press
// shows no status and no hold, the player already walking on it. Judged by the frames' acks.
// From #357's CI: east, into the slimes, a slime can stop the step, and a slam while the reader lags knocks the player back; so west, judged from the frame before.
func TestAStunnedPlayerBreaksFree(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, slamming...))
	token, id := s.CreateCharacter(t, "breaker", "Breaker", "beginner")
	s.Place(t, id, "forest", e2etest.SlimeField, 134, 0)
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Await(300*time.Millisecond, nil)

	var press uint32
	var last, before, after *clientv1.PlayerView // the player's latest frame, the one before the first acking an input past the press, and that one
	answered := false
	for game := l.Game(15 * time.Second); game.Left() && (after == nil || !answered); {
		if err := l.Input(&clientv1.Input{Left: press > 0}); err != nil {
			t.Fatal(err)
		}
		l.Pump(50*time.Millisecond, func(m wsconn.Message) bool {
			switch d := m.Data.(type) {
			case *clientv1.Response:
				if m.ReqID == "free" {
					if !d.GetOk() {
						t.Fatalf("Break Free refused with %d", d.GetCode())
					}
					answered = true
				}
			case *clientv1.Delta:
				for _, v := range d.GetPlayers() {
					if v.GetId() != id {
						continue
					}
					stunned := slices.ContainsFunc(v.GetStatuses(), func(s *clientv1.StatusView) bool { return s.GetId() == "stun" })
					switch {
					case press == 0 && stunned && v.GetHeld() > 0:
						press = l.Seq
						l.Send(wsconn.Message{ReqID: "free", Seq: press, Data: &clientv1.UseSkill{Skill: "break_free", ChainRoot: "roll_back"}})
					case press > 0 && d.GetAckSeq() > press && after == nil:
						before, after = last, v
					}
					last = v
				}
			}
			return false
		})
	}
	if press == 0 || !answered || after == nil {
		t.Fatalf("pressed during input %d, answered %v, a frame past the press %v, within 15 s", press, answered, after != nil)
	}
	if len(after.GetStatuses()) != 0 || after.GetHeld() != 0 || after.GetX() >= before.GetX() {
		t.Fatalf("the first frame acking past the press's input %d: statuses %v, held %d, x %v from %v; want free, walking west", press, after.GetStatuses(), after.GetHeld(), after.GetX(), before.GetX())
	}
}
