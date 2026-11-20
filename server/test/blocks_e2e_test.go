package e2e_test

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// longGuard gives the Beginner's Guard 4 s, so the slimes' blows fall in it: the same block, its window only longer.
var longGuard = []e2etest.DataEdit{
	{File: "skills.yaml", Old: "    cooldown: 6s\n    length: 600ms\n    block: 600ms\n", New: "    cooldown: 1s\n    length: 4s\n    block: 4s\n"},
}

// guardTicks is longGuard's 4 s in the forest's 20 ticks a second.
const guardTicks = 80

// C21: a Beginner's Guard aimed east, at the slimes' circle, blocks their blows: BLOCK crosses the wire with no damage, and the use gives 2 energy once. Aimed west, the same blows come from behind and land.
// Each blow is judged by where its slime stands in the frame of its tick, so one that came round the player's side counts as it came; the guard's window is its SkillUsed's tick on.
func TestAGuardBlocksSlimesInFrontAndNotBehind(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, longGuard...))
	token, id := s.CreateCharacter(t, "guard", "Guard", "beginner")
	if _, err := s.DB.Exec(context.Background(), `UPDATE characters SET energy = 0 WHERE id = $1`, id); err != nil {
		t.Fatal(err)
	}
	s.Place(t, id, "forest", e2etest.SlimeField, 134, 0)
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Await(300*time.Millisecond, nil)

	type use struct {
		aim      float32
		from     uint64 // its SkillUsed's tick, 0 until then
		blocked  int    // BLOCKs in its window, wherever their slime stood
		landed   int    // blows from behind that landed
		answered bool
	}
	var uses []*use
	energy := -1
	var now uint64 // the newest frame's tick
	guard := func(aim float32) {
		u := &use{aim: aim}
		uses = append(uses, u)
		l.Send(wsconn.Message{ReqID: "guard" + strconv.Itoa(len(uses)), Seq: l.Seq, Data: &clientv1.UseSkill{Skill: "guard", ChainRoot: "guard", Aim: aim}})
	}
	// front is how squarely the blow's slime stands in front of the aim: 1 ahead, -1 behind, near 0 at the edge.
	front := func(aim float32, mob *clientv1.MobView) float64 {
		dx, dz := float64(mob.GetX())-l.X, float64(mob.GetZ())-l.Z
		hx, hz := math.Sincos(float64(aim))
		return (hx*dx + hz*dz) / math.Hypot(dx, dz)
	}
	const east, west = float32(math.Pi / 2), float32(-math.Pi / 2)
	guard(east)
guarding:
	for game := l.Game(25 * time.Second); game.Left(); {
		last := uses[len(uses)-1]
		switch {
		case last.aim == east && last.blocked > 0 && last.from > 0 && now >= last.from+guardTicks:
			guard(west) // blocked from the front: now turn away
		case last.aim == west && last.landed > 0:
			break guarding
		case last.from > 0 && now >= last.from+guardTicks:
			guard(last.aim) // no blow fell in it: again
		}
		if err := l.Input(&clientv1.Input{}); err != nil {
			t.Fatal(err)
		}
		l.Pump(50*time.Millisecond, func(m wsconn.Message) bool {
			switch d := m.Data.(type) {
			case *clientv1.Response:
				if n, ok := strings.CutPrefix(m.ReqID, "guard"); ok {
					k, _ := strconv.Atoi(n)
					if !d.GetOk() {
						t.Fatalf("Guard %d refused with %d", k, d.GetCode())
					}
					uses[k-1].answered = true
				}
			case *clientv1.SkillUsed:
				if d.GetCaster() == id && d.GetSkill() == "guard" {
					for _, u := range uses {
						if u.from == 0 {
							u.from = m.Tick
							break
						}
					}
				}
			case *clientv1.Delta:
				now = m.Tick
				if d.Energy != nil {
					energy = int(d.GetEnergy())
				}
			case *clientv1.Damage:
				if d.GetTarget() != "p:"+id || !strings.HasPrefix(d.GetSource(), "m:") {
					return false
				}
				mobID, _ := strconv.ParseUint(strings.TrimPrefix(d.GetSource(), "m:"), 10, 32)
				mob := l.Mobs[uint32(mobID)]
				for _, u := range uses {
					if u.from == 0 || m.Tick < u.from || m.Tick >= u.from+guardTicks {
						continue
					}
					if d.GetBlock() {
						u.blocked++
					}
					if mob == nil {
						continue
					}
					switch f := front(u.aim, mob); { // near the edge, where the slime stood and where its blow came from may part, it isn't judged
					case f > 0.2 && !d.GetBlock(), f < -0.2 && d.GetBlock():
						t.Fatalf("a blow at tick %d from a slime %.2f in front of the guard's aim: %+v; want blocked only in front", m.Tick, f, d)
					case f < -0.2:
						u.landed++
					}
				}
			}
			return false
		})
	}
	var blocked, landed, gave int
	for _, u := range uses {
		if u.aim == east {
			blocked += u.blocked
		} else {
			landed += u.landed
		}
		if u.blocked > 0 {
			gave += 2
		}
	}
	if blocked == 0 || landed == 0 {
		t.Fatalf("uses %+v: %d blows blocked facing the slimes, %d landed from behind; want both", uses, blocked, landed)
	}
	if energy != gave {
		t.Fatalf("energy %d after %d blocks over the uses %+v; want 2 for each use that blocked, once", energy, blocked, uses)
	}
}
