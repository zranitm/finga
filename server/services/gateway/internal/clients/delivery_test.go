package clients

import (
	"testing"

	worldv1 "finga/api/gen/world/v1"
	"finga/services/gateway/internal/model"
)

// A StatsChanged without a sheet stays without one: reading it as a zeroed sheet would wipe the client's stats.
func TestAStatsChangedKeepsAnUnsetSheetUnset(t *testing.T) {
	push, ok := worldPush(&worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_StatsChanged{StatsChanged: &worldv1.StatsChanged{Hp: 50, MaxHp: 135}}})
	if sc, is := push.(model.StatsChanged); !ok || !is || sc.Sheet != nil || sc.HP != 50 || sc.MaxHP != 135 {
		t.Fatalf("got %+v, want HP and MP with no sheet", push)
	}
	push, ok = worldPush(&worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_StatsChanged{StatsChanged: &worldv1.StatsChanged{
		Sheet: &worldv1.StatSheet{Primary: &worldv1.Stats{Strength: 11}, Points: &worldv1.Points{Unspent: 4}}, Hp: 50, MaxHp: 208,
	}}})
	if sc, is := push.(model.StatsChanged); !ok || !is || sc.Sheet == nil || sc.Sheet.Primary.STR != 11 || sc.Sheet.Points.Unspent != 4 {
		t.Fatalf("got %+v, want the sheet carried", push)
	}
}

// A mob's pose comes through, so a player who joins sees a downed mob.
func TestAMobCarriesItsPose(t *testing.T) {
	for pose, want := range map[worldv1.MobPose]model.MobPose{
		worldv1.MobPose_MOB_POSE_UNSPECIFIED: model.Standing,
		worldv1.MobPose_MOB_POSE_FLINCH:      model.Flinching,
		worldv1.MobPose_MOB_POSE_DOWN:        model.Down,
		worldv1.MobPose_MOB_POSE_RISING:      model.Rising,
	} {
		push, ok := worldPush(&worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_Snapshot{Snapshot: &worldv1.Snapshot{Mobs: []*worldv1.MobView{{Id: 4, Pose: pose}}}}})
		if s, is := push.(model.Snapshot); !ok || !is || len(s.Mobs) != 1 || s.Mobs[0].Pose != want {
			t.Errorf("%v: got %+v, want a mob in pose %d", pose, push, want)
		}
	}
}

// A Snapshot carries the map's field boss while one is up, and none otherwise.
func TestASnapshotCarriesTheFieldBossWhileOneIsUp(t *testing.T) {
	push, ok := worldPush(&worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_Snapshot{Snapshot: &worldv1.Snapshot{FieldBoss: &worldv1.FieldBoss{MobId: 9, Type: "king_slime", LeavesInMs: 420000}}}})
	if s, is := push.(model.Snapshot); !ok || !is || s.FieldBoss == nil || *s.FieldBoss != (model.FieldBoss{MobID: 9, Type: "king_slime", LeavesInMs: 420000}) {
		t.Fatalf("got %+v, want the king leaving in 7 minutes", push)
	}
	push, ok = worldPush(&worldv1.DeliverResponse{Event: &worldv1.DeliverResponse_Snapshot{Snapshot: &worldv1.Snapshot{MapId: "forest"}}})
	if s, is := push.(model.Snapshot); !ok || !is || s.FieldBoss != nil {
		t.Fatalf("got %+v, want no boss", push)
	}
}
