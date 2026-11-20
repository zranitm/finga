package e2e

import (
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/dependencies"
	"finga/internal/shared/gamedata"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// Step 6a (large-regions.md section 8): a character in the test region 4 m from its town's stone finds it on joining; the next login's Welcome lists it, and one after a restart, read from the database.
func TestAWaypointFoundIsInTheNextWelcome(t *testing.T) {
	t.Parallel()
	s := e2etest.NewStackBuilt(t, "../data", dependencies.Build)
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
