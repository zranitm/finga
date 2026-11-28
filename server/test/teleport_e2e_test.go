package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// B9: a player teleports from Willowbrook to Frost Trail and back, the map by its id to its player spawn and the region by its main town's waypoint to beside the stone, each landing no sooner than the channel allows, and the database keeps the last.
func TestTeleportFromTheTownToAMapAndBack(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, e2etest.DataEdit{File: "world.yaml", Old: "channel: 3s", New: "channel: 1s"})) // twenty ticks still tell a landing before its channel ends
	data, err := load.Dir(s.Data)
	if err != nil {
		t.Fatal(err)
	}
	token, id := s.CreateCharacter(t, "tam", "Tam", "warrior")
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Await(300*time.Millisecond, nil)

	trail, _ := data.Map("frost_trail")
	_, stone, _ := data.Waypoint("willowbrook")
	for _, step := range []struct {
		req    *clientv1.Teleport
		mapID  string
		at     gamedata.Point
		facing float64
	}{
		{&clientv1.Teleport{MapId: "frost_trail"}, "frost_trail", trail.PlayerSpawn, trail.SpawnYaw},
		{&clientv1.Teleport{Waypoint: "willowbrook"}, "willowmere", stone.Arrive, stone.Yaw},
	} {
		sent := time.Now() // the channel starts on the first tick to drain the request, after this
		if r := l.Teleport(t, "tp-"+step.mapID, step.req); !r.GetOk() {
			t.Fatalf("teleport to %s refused with %d", step.mapID, r.GetCode())
		}
		var mc *clientv1.MapChange
		l.Await(data.TeleportChannel()+3*time.Second, func(m wsconn.Message) bool {
			mc, _ = m.Data.(*clientv1.MapChange)
			return mc != nil
		})
		took := time.Since(sent)
		if mc == nil {
			t.Fatalf("no MapChange to %s", step.mapID)
		}
		if mc.GetMapId() != step.mapID || mc.GetX() != step.at.X || mc.GetZ() != step.at.Z || mc.GetYaw() != float32(step.facing) {
			t.Fatalf("map change %+v, want %s at %+v facing %v", mc, step.mapID, step.at, step.facing)
		}
		// Ticks keep a fixed grid, so the start's tick may run up to a tick late and the end's on time.
		if least := data.TeleportChannel() - s.Cfg.World.TickInterval(); took < least {
			t.Fatalf("landed %v after the request, under the %v channel less a tick", took, data.TeleportChannel())
		}
		if !l.Pump(5*time.Second, func(m wsconn.Message) bool { return m.Type == "Snapshot" }) {
			t.Fatalf("no Snapshot on %s", step.mapID)
		}
	}
	var mapID string
	var x, z float64
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if err := s.DB.QueryRow(context.Background(), `SELECT map_id, pos_x, pos_z FROM characters WHERE id = $1`, id).Scan(&mapID, &x, &z); err != nil {
			t.Fatal(err)
		}
		if mapID == "willowmere" && x == stone.Arrive.X && z == stone.Arrive.Z {
			return
		}
	}
	t.Fatalf("saved on %s at (%v, %v), want Willowbrook's stone at %+v", mapID, x, z, stone.Arrive)
}

// B9: a mob's hit landing on a channelling player cancels the teleport, as HIT on the hit's tick; a MISS before it doesn't.
func TestAMobsHitCancelsATeleport(t *testing.T) {
	t.Parallel()
	// A channel long enough that Sunmeadow's slimes surely reach the player in it.
	s := newStackOver(t, e2etest.GameData(t, e2etest.DataEdit{File: "world.yaml", Old: "channel: 3s", New: "channel: 10s"}))
	token, id := s.CreateCharacter(t, "hal", "Hal", "warrior")
	s.Place(t, id, "willowmere", e2etest.Spots(t).SlimeField, 134, 0)
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Await(300*time.Millisecond, nil)

	if r := l.Teleport(t, "tp-trail", &clientv1.Teleport{MapId: "frost_trail"}); !r.GetOk() {
		t.Fatalf("teleport refused with %d", r.GetCode())
	}
	var hit uint64
	var cancel *clientv1.TeleportCancelled
	var at uint64
	l.Await(12*time.Second, func(m wsconn.Message) bool {
		switch d := m.Data.(type) {
		case *clientv1.Damage:
			if d.GetTarget() == "p:"+id && !d.GetMiss() && hit == 0 {
				hit = m.Tick
			}
		case *clientv1.TeleportCancelled:
			cancel, at = d, m.Tick
			return true
		case *clientv1.MapChange:
			t.Fatalf("teleported to %s; want the channel cancelled by a slime's hit", d.GetMapId())
		}
		return false
	})
	if cancel == nil || cancel.GetReason() != clientv1.TeleportCancelReason_TELEPORT_CANCEL_REASON_HIT || hit == 0 || at != hit {
		t.Fatalf("cancel %+v on tick %d, the first landed hit on tick %d; want HIT on the hit's tick", cancel, at, hit)
	}
}
