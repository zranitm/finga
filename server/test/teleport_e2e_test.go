package e2e_test

import (
	"context"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata/load"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// B9: a player teleports from the town to the forest and back, each landing at the map's player spawn no sooner than the channel allows, and the database keeps the last.
func TestTeleportFromTheTownToTheForestAndBack(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, e2etest.DataEdit{File: "world.yaml", Old: "channel: 3s", New: "channel: 1s"})) // twenty ticks still tell a landing before its channel ends
	data, err := load.Dir(s.Data)
	if err != nil {
		t.Fatal(err)
	}
	token, id := s.CreateCharacter(t, "tam", "Tam", "warrior")
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Pump(300*time.Millisecond, nil)

	for _, to := range []string{"forest", "town"} {
		target, _ := data.Map(to)
		sent := time.Now() // the channel starts on the first tick to drain the request, after this
		if r := l.Teleport(t, "tp-"+to, to); !r.GetOk() {
			t.Fatalf("teleport to %s refused with %d", to, r.GetCode())
		}
		var mc *clientv1.MapChange
		l.Await(data.TeleportChannel()+3*time.Second, func(m wsconn.Message) bool {
			mc, _ = m.Data.(*clientv1.MapChange)
			return mc != nil
		})
		took := time.Since(sent)
		if mc == nil {
			t.Fatalf("no MapChange to %s", to)
		}
		if mc.GetMapId() != to || mc.GetX() != target.PlayerSpawn.X || mc.GetZ() != target.PlayerSpawn.Z || mc.GetYaw() != float32(target.SpawnYaw) {
			t.Fatalf("map change %+v, want %s's spawn %+v at yaw %v", mc, to, target.PlayerSpawn, target.SpawnYaw)
		}
		// Ticks keep a fixed grid, so the start's tick may run up to a tick late and the end's on time.
		if least := data.TeleportChannel() - s.Cfg.World.TickInterval(); took < least {
			t.Fatalf("landed %v after the request, under the %v channel less a tick", took, data.TeleportChannel())
		}
		if !l.Pump(5*time.Second, func(m wsconn.Message) bool { return m.Type == "Snapshot" }) {
			t.Fatalf("no Snapshot on %s", to)
		}
	}
	town, _ := data.Map("town")
	var mapID string
	var x, z float64
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(100 * time.Millisecond) {
		if err := s.DB.QueryRow(context.Background(), `SELECT map_id, pos_x, pos_z FROM characters WHERE id = $1`, id).Scan(&mapID, &x, &z); err != nil {
			t.Fatal(err)
		}
		if mapID == "town" && x == town.PlayerSpawn.X && z == town.PlayerSpawn.Z {
			return
		}
	}
	t.Fatalf("saved on %s at (%v, %v), want the town's spawn %+v", mapID, x, z, town.PlayerSpawn)
}

// B9: a mob's hit landing on a channelling player cancels the teleport, as HIT on the hit's tick; a MISS before it doesn't.
func TestAMobsHitCancelsATeleport(t *testing.T) {
	t.Parallel()
	// A channel long enough that the forest's slimes surely reach the player in it.
	s := newStackOver(t, e2etest.GameData(t, e2etest.DataEdit{File: "world.yaml", Old: "channel: 3s", New: "channel: 10s"}))
	token, id := s.CreateCharacter(t, "hal", "Hal", "warrior")
	s.Place(t, id, "forest", e2etest.SlimeField, 134, 0)
	l := e2etest.NewLooter(s.Join(t, token, id))
	l.Pump(300*time.Millisecond, nil)

	if r := l.Teleport(t, "tp-town", "town"); !r.GetOk() {
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
