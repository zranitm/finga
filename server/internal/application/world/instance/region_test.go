package instance_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/instance"
	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
)

// A region is its own town (large-regions.md section 4): one who dies in it gets up at its start, in it and in their channel, with no hand-off, having paid the death's EXP as on a hunting map.
func TestAPlayerWhoDiesInARegionGetsUpAtItsStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := arena(t, time.Hour)
		m.ID, m.Kind, m.Town = "vale", gamedata.Region, "vale"
		m.PlayerSpawn, m.SpawnYaw = gamedata.Point{X: 5, Z: 30}, 1.5
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, data, m, &worldtest.Checkpoints{}, worldtest.Options{Transfer: tr, Channel: 2})
		defer stop()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		p := warriorAt(nextToSlime)
		p.HP, p.Exp = 1, 10
		s := join(t, inst, p)
		untilDead(t, s, p.ID)
		time.Sleep(3 * time.Second)
		synctest.Wait()
		got := eventsOf[model.Respawned](s)
		if len(got) != 1 || got[0].MapID != "vale" || got[0].X != 5 || got[0].Z != 30 || got[0].Yaw != 1.5 {
			t.Fatalf("respawned %+v; want once in vale at its start", got)
		}
		if v, ok := s.Own(p.ID); !ok || v.Dead || v.State.X != 5 || v.State.Z != 30 {
			t.Fatalf("after getting up: %+v", v)
		}
		if lost := eventsOf[model.ExpGained](s); len(lost) != 1 || lost[0].Amount != -1 {
			t.Errorf("EXP %+v; want the death's 1 lost", lost)
		}
		tr.mu.Lock()
		defer tr.mu.Unlock()
		if tel := eventsOf[model.Teleport](s); len(tel) != 0 || len(tr.moved) != 0 {
			t.Errorf("handed off: teleports %+v, moved to %v", tel, tr.to)
		}
	})
}

// The test region is reached by a teleport to its id, which lands at its start (large-regions.md section 12).
func TestATeleportToTheTestRegionLandsAtItsStart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		tr := &transferRecorder{}
		inst, stop := worldtest.Run(t, data, &worldtest.Checkpoints{}, worldtest.Options{Transfer: tr, CheckpointInterval: time.Hour})
		defer stop()
		p := atSpawn(data, "Ayla")
		p.Connection = "c1"
		s := join(t, inst, p)
		teleport(t, inst, s, p.ID, "test_region", 0)
		tick(channelTicks(data))
		region, _ := data.Map("test_region")
		got := eventsOf[model.Teleport](s)
		if len(got) != 1 || got[0].MapID != "test_region" || got[0].X != region.PlayerSpawn.X || got[0].Z != region.PlayerSpawn.Z || region.Kind != gamedata.Region {
			t.Fatalf("teleports %+v; want one to the test region's start %+v", got, region.PlayerSpawn)
		}
		tr.mu.Lock()
		defer tr.mu.Unlock()
		if len(tr.to) != 1 || tr.to[0] != "test_region" {
			t.Errorf("moved to %v", tr.to)
		}
	})
}

// One going into a match from a region's channel keeps it as their way back, and goes back to it (large-regions.md section 4): the World then lands them there while it has room.
func TestAMatchFromARegionsChannelGoesBackToIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := arena(t, time.Hour)
		m.ID, m.Kind, m.Town = "vale", gamedata.Region, "vale"
		tr := &transferRecorder{}
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{Transfer: tr, Channel: 2})
		defer stop()
		p := warriorAt(outOfSight)
		p.Connection = "c1"
		join(t, inst, p)
		ring, _ := data.Map("ring")
		if err := inst.ToMatch(context.Background(), p.ID, "m-1", ring.ID, ring.Sides[0][0]); err != nil {
			t.Fatal(err)
		}
		tr.mu.Lock()
		if len(tr.moved) != 1 || tr.dests[0] != (instance.Dest{ID: "m-1"}) || tr.moved[0].Back == nil || tr.moved[0].Back.MapID != "vale" || tr.moved[0].Back.Channel != 2 {
			t.Fatalf("hand-offs to %+v, back %+v; want the match, back to vale's channel 2", tr.dests, tr.moved)
		}
		tr.mu.Unlock()

		results, moves := &worldtest.Results{}, &worldtest.Moves{}
		ayla := fighterFrom(t, data, "Ayla")
		ayla.Back.MapID, ayla.Back.Channel = "test_region", 2
		match, stopMatch := worldtest.RunMatch(t, data, duel(model.Fighter{Character: ayla.ID}, model.Fighter{Bot: botOf(data, "Ash")}), nil, worldtest.Options{Results: results, Transfer: moves})
		defer stopMatch()
		if err := match.Join(context.Background(), ayla, &worldtest.Sink{}); err != nil {
			t.Fatal(err)
		}
		sleep(worldtest.TickInterval)
		if err := match.Forfeit(context.Background(), ayla.ID, ayla.Connection); err != nil {
			t.Fatal(err)
		}
		sleep(worldtest.TickInterval)
		if mv := moves.All(); len(mv) != 1 || mv[0].Dest != (instance.Dest{ID: "test_region", Channel: 2}) {
			t.Fatalf("hand-offs %+v, want Ayla back to the test region's channel 2", mv)
		}
	})
}

// A region's channel reaches the party frame and the tick metrics: a join tells the party the channel, and a hand-off off the region tells none, as the map it lands on tells its own.
func TestARegionsChannelReachesThePartyAndTheTicks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		m := arena(t, time.Hour)
		m.ID, m.Kind, m.Town = "vale", gamedata.Region, "vale"
		parties, ticks := &fakeParties{}, &worldtest.Observer{}
		inst, stop := worldtest.RunMap(t, data, m, nil, worldtest.Options{Parties: parties, Observer: ticks, Transfer: &transferRecorder{}, Channel: 2})
		defer stop()
		p := warriorAt(outOfSight)
		p.Connection = "c1"
		s := join(t, inst, p)
		tick(1)
		if got := parties.updated(); len(got) == 0 || got[0].MapID != "vale" || got[0].Channel != 2 || ticks.Channel() != 2 {
			t.Fatalf("party updates %+v, ticks of channel %d; want vale's channel 2 for both", got, ticks.Channel())
		}
		teleport(t, inst, s, p.ID, "forest", 0)
		tick(channelTicks(data) + 1)
		if got := parties.updated(); got[len(got)-1].MapID != "forest" || got[len(got)-1].Channel != 0 {
			t.Fatalf("the hand-off told %+v; want the forest, with no channel", got[len(got)-1])
		}
	})
}
