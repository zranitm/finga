package e2e_test

import (
	"context"
	"strings"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// hopBack teaches the Beginner Hop Back: a leap of up to 4 m back over 300 ms, dodging for 10 s from its start.
// It passes through mobs: a leap that stops at bodies, as the shipped escapes' do, ends on its first sub-step against a slime already touching the player.
var hopBack = []e2etest.DataEdit{
	{File: "skills.yaml", Old: "skills:\n  - id: strike\n", New: `skills:
  - id: hop_back
    icon: lorc/dodging
    kind: active
    effect: move
    energy: 2
    cooldown: 1s
    length: 600ms
    move: {kind: leap, distance: 4, time: 300ms, toward: back, through: true}
    dodge: 10s
    stages:
      - {at: 0, name: Hop Back, description: "Hops back, dodging."}
  - id: strike
`},
	{File: "classes.yaml", Old: "skills: [strike, throw, spark, second_wind, lunge, roll_back, break_free, guard]", New: "skills: [strike, throw, spark, second_wind, lunge, roll_back, break_free, guard, hop_back]"},
}

// C17: a moving skill's way crosses the wire, its leap shows in the player's frames from the input after the press, and the slimes' attacks pass through its dodge with no damage.
// It's pressed once a slime has attacked, so the slimes are on the player, and the dodge is judged over its own window in the world's ticks, which a slow runner's clock can't cut short.
// The dodge counts from the tick the skill is used, so a bite before it is left out.
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
	s.Place(t, id, "willowmere", e2etest.Spots(t).SlimeField, 134, 0)
	l := e2etest.NewLooter(s.Join(t, token, id))
	ticks := func(d time.Duration) uint64 { return uint64(d / s.Cfg.World.TickInterval()) } //nolint:gosec // seconds of ticks

	var now, first uint64 // the newest frame's tick, and the first's
	bitten := false
	for game := l.Game(30 * time.Second); !bitten; {
		if !game.Left() {
			t.Fatalf("no slime attacked the player in ticks %d to %d", first, now)
		}
		if err := l.Input(&clientv1.Input{}); err != nil {
			t.Fatal(err)
		}
		l.Pump(50*time.Millisecond, func(m wsconn.Message) bool {
			switch d := m.Data.(type) {
			case *clientv1.Delta:
				now = m.Tick
				if first == 0 {
					first = m.Tick
				}
			case *clientv1.Damage:
				bitten = bitten || d.GetTarget() == "p:"+id && strings.HasPrefix(d.GetSource(), "m:")
			}
			return false
		})
	}

	// The leap goes 1 m south, passing through any slime come round behind the player.
	if err := l.Input(&clientv1.Input{}); err != nil {
		t.Fatal(err)
	}
	l.Send(wsconn.Message{ReqID: "hop", Seq: l.Seq, Data: &clientv1.UseSkill{Skill: "hop_back", Way: &clientv1.SkillWay{Z: -1, Length: 1}}})
	pressed := now
	answered, leapt, dodging := false, false, false
	var used uint64
	var hp int32 // as the skill is used: its tick's frame comes before its events
	var dodged []*clientv1.Damage
	window := ticks(10 * time.Second) // the dodge's
	done := func() bool { return answered && leapt && dodging && len(dodged) >= 2 }
	for game := l.Game(time.Minute); !done(); { // the budget only guards against a world gone quiet: the ticks below end it sooner
		if used == 0 && now >= pressed+ticks(2*time.Second) || used > 0 && now >= used+window || !game.Left() {
			break
		}
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
				now = m.Tick
				for _, v := range d.GetPlayers() {
					if v.GetId() == id {
						leapt = leapt || v.GetMove().GetLeft() > 0 && v.GetMove().GetVz() < 0
						dodging = dodging || v.GetDodging()
					}
				}
			case *clientv1.Damage:
				if d.GetTarget() == "p:"+id && used > 0 && m.Tick >= used && m.Tick < used+window {
					if !d.GetDodge() || d.GetAmount() != 0 {
						t.Fatalf("a slime's attack in the dodge %+v, want it passing through", d)
					}
					dodged = append(dodged, d)
				}
			}
			return done() // stops before a bite past the dodge can change the HP
		})
	}
	if !done() {
		t.Fatalf("by tick %d, the skill used at %d: answered %v, a frame of the leap southward %v, of the dodge %v, %d attacks dodged; want all, and two", now, used, answered, leapt, dodging, len(dodged))
	}
	if l.HP < hp { // the bite before the press leaves room for regen to add
		t.Fatalf("HP %d after the dodged attacks, want at least the %d at the press", l.HP, hp)
	}
}

// C17: a skill pressed mid-swing waits for the attack's cancel point, the server holding the inputs after the press until it's answered, so its move starts from the input after the press, as the client marks it.
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
	s.Place(t, id, "willowmere", e2etest.Spots(t).OutOfSight, 134, 0)
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Await(300*time.Millisecond, nil)

	if err := l.Input(&clientv1.Input{}); err != nil {
		t.Fatal(err)
	}
	l.Send(wsconn.Message{Data: &clientv1.Attack{}})
	for range 2 { // the swing under way
		if err := l.Input(&clientv1.Input{}); err != nil {
			t.Fatal(err)
		}
		l.Await(50*time.Millisecond, nil)
	}
	press := l.Seq // pressed two inputs into the swing, before its cancel point
	l.Send(wsconn.Message{ReqID: "hop", Seq: press, Data: &clientv1.UseSkill{Skill: "hop_back", Way: &clientv1.SkillWay{Z: -1, Length: 1}}})
	var pressed, used uint64
	var answered bool
	var after *clientv1.PlayerView // the player in the first frame acking an input past the press
	var afterAck uint32
	for game := l.Game(10 * time.Second); game.Left() && (after == nil || !answered); {
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
