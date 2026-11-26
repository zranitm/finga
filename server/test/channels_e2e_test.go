package e2e

import (
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/config"
	"finga/internal/dependencies"
	"finga/internal/shared/gamedata"
	"finga/pkg/wsconn"
	"finga/test/e2etest"
)

// A region's channels through the stack (large-regions.md section 4): with two players a channel, a third joins channel 2, the list shows both, and a switch to 2 lands where the player stood, among channel 2's players, who see them come.
func TestARegionsChannels(t *testing.T) {
	t.Parallel()
	s := e2etest.NewStackBuilt(t, e2etest.GameData(t), dependencies.Build, func(c *config.Config) { c.World.Channels.Cap = 2 })
	join := func(user, name string, at gamedata.Point) (*e2etest.Looter, *clientv1.Snapshot, string) {
		token, id := s.CreateCharacter(t, user, name, "warrior")
		s.Place(t, id, "test_region", at, 100, 0)
		p, snap := s.Join(t, token, id)
		return e2etest.NewLooter(p, snap), snap, id
	}
	start := gamedata.Point{X: 124, Z: 80}
	ann, sa, annID := join("ann", "Ann", start)
	_, sb, _ := join("bobo", "Bob", gamedata.Point{X: 126, Z: 80})
	cy, sc, _ := join("cyan", "Cyd", gamedata.Point{X: 122, Z: 80})
	if got := []uint32{sa.GetChannel(), sb.GetChannel(), sc.GetChannel()}; !slices.Equal(got, []uint32{1, 1, 2}) {
		t.Fatalf("joined channels %v; want Ann and Bo in 1, Cy in 2", got)
	}
	list := ann.Channels(t, "c-1").GetChannels()
	if list.GetCap() != 2 || len(list.GetChannels()) != 2 || list.GetChannels()[0].GetPlayers() != 2 || list.GetChannels()[1].GetPlayers() != 1 {
		t.Fatalf("channels %+v; want 1 with two, 2 with one, of 2", list)
	}
	if r := ann.SwitchChannel(t, "s-1", 1); r.GetOk() {
		t.Fatal("a switch to Ann's own channel went")
	}
	if r := ann.SwitchChannel(t, "s-2", 2); !r.GetOk() {
		t.Fatalf("the switch to 2: %+v", r)
	}
	var change *clientv1.MapChange
	var snap *clientv1.Snapshot
	if !ann.Pump(10*time.Second, func(m wsconn.Message) bool {
		switch d := m.Data.(type) {
		case *clientv1.MapChange:
			change = d
		case *clientv1.Snapshot:
			snap = d
		}
		return snap != nil
	}) || change == nil {
		t.Fatal("no MapChange and Snapshot after the switch")
	}
	if change.GetMapId() != "test_region" || change.GetX() != start.X || change.GetZ() != start.Z || snap.GetChannel() != 2 {
		t.Fatalf("map change %+v, snapshot channel %d; want the test region where Ann stood, channel 2", change, snap.GetChannel())
	}
	if !slices.ContainsFunc(snap.GetPlayers(), func(v *clientv1.PlayerView) bool { return v.GetName() == "Cyd" }) {
		t.Fatalf("channel 2's snapshot has %+v; want Cy", snap.GetPlayers())
	}
	if !cy.Pump(5*time.Second, func(m wsconn.Message) bool {
		d, ok := m.Data.(*clientv1.Delta)
		return ok && slices.ContainsFunc(d.GetPlayers(), func(v *clientv1.PlayerView) bool { return v.GetId() == annID })
	}) {
		t.Fatal("Cy didn't see Ann come")
	}
}
