package e2e_test

import (
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// Step 6a (large-regions.md section 8): a character in the test region 4 m from its town's stone finds it on joining; the next login's Welcome lists it, and one after a restart, read from the database.
func TestAWaypointFoundIsInTheNextWelcome(t *testing.T) {
	t.Parallel()
	s := newStack(t)
	token, id := s.CreateCharacter(t, "wren", "Wren", "warrior")
	s.Place(t, id, "test_region", gamedata.Point{X: 124, Z: 81}, 100, 0)
	l, w := s.Hello(t, token, id)
	if got := w.GetWaypoints(); len(got) != 0 {
		t.Fatalf("a new character's Welcome lists %v", got)
	}
	if !l.Await(5*time.Second, func(m wsconn.Message) bool {
		f, ok := m.Data.(*clientv1.WaypointFound)
		return ok && f.GetWaypoint() == "town"
	}) {
		t.Fatal("no WaypointFound for the town's stone")
	}
	l, again := s.Hello(t, w.GetToken(), id)
	if got := again.GetWaypoints(); !slices.Equal(got, []string{"town"}) {
		t.Fatalf("the next login's Welcome lists %v; want the town's", got)
	}
	s.Restart()
	if _, w = s.Hello(t, l.Token, id); !slices.Equal(w.GetWaypoints(), []string{"town"}) {
		t.Fatalf("after a restart, the Welcome lists %v; want the town's", w.GetWaypoints())
	}
}

// Step 6b (large-regions.md section 8): one who found the west field's stone is refused an unfound field and the region by its map id, then teleports to the test region's main town, unfound, and back to the west field, each time landing beside the stone, facing away.
func TestATeleportGoesToAFoundWaypointOrTheMainTown(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, e2etest.DataEdit{File: "world.yaml", Old: "channel: 3s", New: "channel: 1s"}))
	data, err := load.Dir(s.Data)
	if err != nil {
		t.Fatal(err)
	}
	token, id := s.CreateCharacter(t, "wynn", "Wynn", "warrior")
	s.Place(t, id, "test_region", gamedata.Point{X: 60, Z: 81}, 100, 0)
	l, _ := s.Hello(t, token, id)
	if !l.Await(5*time.Second, func(m wsconn.Message) bool {
		f, ok := m.Data.(*clientv1.WaypointFound)
		return ok && f.GetWaypoint() == "west"
	}) {
		t.Fatal("no WaypointFound for the west field's stone")
	}
	// One refusal before each teleport: the two share a limit of two at once, which each landing's 1 s channel fills again.
	for _, step := range []struct {
		refused *clientv1.Teleport
		why     string
		to      string
	}{
		{&clientv1.Teleport{Waypoint: "north"}, "TELEPORT_NOT_FOUND", "town"},
		{&clientv1.Teleport{}, "TELEPORT_UNKNOWN_WAYPOINT", "west"},
	} {
		if r := l.Teleport(t, step.why, step.refused); r.GetOk() || r.GetCode() != code(step.why) {
			t.Fatalf("teleport %v answered ok %v with %d; want %s", step.refused, r.GetOk(), r.GetCode(), step.why)
		}
		to := step.to
		_, w, _ := data.Waypoint(to)
		if r := l.Teleport(t, "tp-"+to, &clientv1.Teleport{Waypoint: to}); !r.GetOk() {
			t.Fatalf("teleport to %s refused with %d", to, r.GetCode())
		}
		var mc *clientv1.MapChange
		l.Await(data.TeleportChannel()+3*time.Second, func(m wsconn.Message) bool {
			mc, _ = m.Data.(*clientv1.MapChange)
			return mc != nil
		})
		if mc == nil || mc.GetMapId() != "test_region" || mc.GetX() != w.Arrive.X || mc.GetZ() != w.Arrive.Z || mc.GetYaw() != float32(w.Yaw) {
			t.Fatalf("map change %+v; want the test region beside %s, at %+v facing %v", mc, to, w.Arrive, w.Yaw)
		}
		if !l.Await(5*time.Second, func(m wsconn.Message) bool { return m.Type == "Snapshot" }) {
			t.Fatalf("no Snapshot beside %s", to)
		}
	}
}
