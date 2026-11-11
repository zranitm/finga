package clients

import (
	"testing"

	worldv1 "finga/api/gen/world/v1"
	"finga/services/gateway/internal/model"
)

// A StatsChanged without a sheet stays without one: reading it as a zeroed sheet would wipe the client's stats.
func TestAStatsChangedKeepsAnUnsetSheetUnset(t *testing.T) {
	push, ok := worldPush(&worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_StatsChanged{StatsChanged: &worldv1.StatsChanged{Hp: 50, MaxHp: 135, Mp: 11, MaxMp: 33}}})
	if sc, is := push.(model.StatsChanged); !ok || !is || sc.Sheet != nil || sc.HP != 50 || sc.MaxHP != 135 || sc.MP != 11 || sc.MaxMP != 33 {
		t.Fatalf("got %+v, want HP and MP with no sheet", push)
	}
	push, ok = worldPush(&worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_StatsChanged{StatsChanged: &worldv1.StatsChanged{
		Sheet: &worldv1.StatSheet{Primary: &worldv1.Stats{Strength: 11}, Points: &worldv1.Points{Unspent: 4}}, Hp: 50, MaxHp: 208,
	}}})
	if sc, is := push.(model.StatsChanged); !ok || !is || sc.Sheet == nil || sc.Sheet.Primary.STR != 11 || sc.Sheet.Points.Unspent != 4 {
		t.Fatalf("got %+v, want the sheet carried", push)
	}
}
