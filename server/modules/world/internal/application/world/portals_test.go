package world_test

import (
	"context"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/modules/world/internal/model"
	"finga/modules/world/worldtest"
)

func portalNamed(t *testing.T, m *gamedata.Map, name string) gamedata.Portal {
	t.Helper()
	for _, p := range m.Portals {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("map %s has no portal %s", m.ID, name)
	return gamedata.Portal{}
}

// seenIn counts the events of s that carry id, and the deltas that remove it.
func seenIn(s *worldtest.Sink, id kernel.CharacterID) (present, removed int) {
	for _, e := range s.All() {
		switch ev := e.(type) {
		case model.Snapshot:
			for _, v := range ev.Players {
				if v.ID == id {
					present++
				}
			}
		case worldtest.Delta:
			for _, v := range ev.Players {
				if v.ID == id {
					present++
				}
			}
			for _, r := range ev.Removed {
				if r == id {
					removed++
				}
			}
		default: // the rest change nothing on the map
		}
	}
	return present, removed
}

func TestPortals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		saver := &fakeSaver{}
		w := newWorld(t, saver, worldtest.Options{CheckpointInterval: time.Hour})
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()
		defer func() {
			cancel()
			if err := <-runDone; err != nil {
				t.Error(err)
			}
			synctest.Wait()
		}()
		townMap, _ := data.Map("town")
		forestMap, _ := data.Map("forest")
		toForest := portalNamed(t, townMap, "to_forest")
		fromTown := portalNamed(t, forestMap, "from_town")
		arrival := fromTown.Arrival()
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()

		robin := worldtest.NewPlayer("Robin", townMap.PlayerSpawn.X, townMap.PlayerSpawn.Y)
		sr := &worldtest.Sink{}
		w.enter(t, robin, "town", "c-robin", sr)

		{ // a portal takes the player to the linked spot on the target map, with HP as it was
			ayla := worldtest.NewPlayer("Ayla", toForest.Arrival().X, toForest.Arrival().Y)
			ayla.HP = 120
			sa := &worldtest.Sink{}
			w.enter(t, ayla, "town", "c-ayla", sa)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			_ = w.EnterPortal(ayla.ID, "c-ayla")
			time.Sleep(2 * worldtest.TickInterval)
			synctest.Wait()
			tel := worldtest.Of[model.Teleport](sa)
			if len(tel) != 1 || tel[0].MapID != "forest" || tel[0].X != arrival.X || tel[0].Y != arrival.Y {
				t.Fatalf("teleports %+v, want one to the forest at %+v", tel, arrival)
			}
			snaps := worldtest.Of[model.Snapshot](sa)
			if len(snaps) != 2 || snaps[1].MapID != "forest" {
				t.Fatalf("snapshots %+v", snaps)
			}
			var me model.PlayerView
			for _, v := range snaps[1].Players {
				if v.ID == ayla.ID {
					me = v
				}
			}
			if me.ID != ayla.ID || me.State.X != arrival.X || me.State.Y != arrival.Y || me.HP != 120 {
				t.Fatalf("in the forest snapshot: %+v", me)
			}
			// Never out of every instance for more than one tick.
			if snaps[1].Tick > tel[0].Tick+1 {
				t.Fatalf("left the town at tick %d, joined the forest at tick %d", tel[0].Tick, snaps[1].Tick)
			}
			synctest.Wait()
			saver.mu.Lock()
			var cp *model.Checkpoint
			for k := range saver.saved {
				if saver.saved[k].ID == ayla.ID {
					cp = &saver.saved[k]
				}
			}
			saver.mu.Unlock()
			if cp == nil || cp.MapID != "forest" || cp.X != arrival.X || cp.HP != 120 {
				t.Fatalf("checkpoint on the way out: %+v", cp)
			}
			if present, removed := seenIn(sr, ayla.ID); removed != 1 || present == 0 {
				t.Fatalf("robin saw ayla %d times and removed %d times", present, removed)
			}
			for range 20 {
				_ = w.EnterPortal(ayla.ID, "c-other") // she stands on the forest's way back
			}
			time.Sleep(2 * worldtest.TickInterval)
			synctest.Wait()
			if len(worldtest.Of[model.Teleport](sa)) != 1 {
				t.Fatal("a command from a connection that does not hold her moved her")
			}
		}

		{ // off a portal, EnterPortal does nothing
			before := len(sr.All())
			_ = w.EnterPortal(robin.ID, "c-robin")
			time.Sleep(2 * worldtest.TickInterval)
			synctest.Wait()
			if len(worldtest.Of[model.Teleport](sr)) != 0 {
				t.Fatal("teleported from the town spawn")
			}
			if v, ok := sr.Own(robin.ID); ok && v.State.X != townMap.PlayerSpawn.X {
				t.Fatalf("moved: %+v", v)
			}
			if len(sr.All()) <= before {
				t.Fatal("robin stopped receiving deltas")
			}
		}

		{ // spamming EnterPortal: one hop, present in exactly one instance at a time
			cass := worldtest.NewPlayer("Cass", toForest.Arrival().X, toForest.Arrival().Y)
			sc := &worldtest.Sink{}
			w.enter(t, cass, "town", "c-cass", sc)
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			for range 100 {
				_ = w.EnterPortal(cass.ID, "c-cass") // the town takes the first; she has left when the rest arrive
			}
			time.Sleep(3 * worldtest.TickInterval)
			synctest.Wait()
			tel := worldtest.Of[model.Teleport](sc)
			snaps := worldtest.Of[model.Snapshot](sc)
			if len(tel) != 1 || len(snaps) != 2 || snaps[1].MapID != "forest" || snaps[1].Tick > tel[0].Tick+1 {
				t.Fatalf("teleports %+v snapshots %d", tel, len(snaps))
			}
			if _, removed := seenIn(sr, cass.ID); removed != 1 {
				t.Fatalf("the town removed cass %d times", removed)
			}
			// After the teleport the town never sent her another delta.
			for _, e := range sc.All() {
				if d, ok := e.(worldtest.Delta); ok && d.Tick > tel[0].Tick+1 {
					for _, r := range d.Removed {
						if r == cass.ID {
							t.Fatal("cass was removed from a map she was on")
						}
					}
				}
			}
			// The handoff works both ways from the forest's arrival portal.
			_ = w.EnterPortal(cass.ID, "c-cass")
			time.Sleep(3 * worldtest.TickInterval)
			synctest.Wait()
			tel = worldtest.Of[model.Teleport](sc)
			if len(tel) != 2 || tel[1].MapID != "town" || tel[1].X != toForest.Arrival().X {
				t.Fatalf("teleports %+v", tel)
			}
			if sn := worldtest.Of[model.Snapshot](sc); len(sn) != 3 || sn[2].MapID != "town" {
				t.Fatalf("snapshots %d", len(sn))
			}
		}
	})
}

// A second portal press that reaches the target map in the same drain as the landing join takes the player on, not between maps.
func TestASecondPortalInTheLandingDrainTakesThePlayerOn(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		w := newWorld(t, &fakeSaver{}, worldtest.Options{CheckpointInterval: time.Hour})
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()
		defer func() {
			cancel()
			if err := <-runDone; err != nil {
				t.Error(err)
			}
			synctest.Wait()
		}()
		townMap, _ := data.Map("town")
		toForest := portalNamed(t, townMap, "to_forest")
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		cass := worldtest.NewPlayer("Cass", toForest.Arrival().X, toForest.Arrival().Y)
		sc := &worldtest.Sink{}
		w.enter(t, cass, "town", "c-cass", sc)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		_ = w.EnterPortal(cass.ID, "c-cass")
		for len(worldtest.Of[model.Teleport](sc)) == 0 {
			time.Sleep(time.Millisecond)
			synctest.Wait()
		}
		if n := len(worldtest.Of[model.Snapshot](sc)); n != 1 {
			t.Skipf("the forest answered the landing before the second press was routed to it (%d snapshots)", n)
		}
		_ = w.EnterPortal(cass.ID, "c-cass") // routed to the forest, behind the landing join
		time.Sleep(10 * worldtest.TickInterval)
		synctest.Wait()
		var maps []string
		for _, s := range worldtest.Of[model.Snapshot](sc) {
			maps = append(maps, s.MapID)
		}
		if tel := worldtest.Of[model.Teleport](sc); len(tel) != 2 || !slices.Equal(maps, []string{"town", "forest", "town"}) {
			t.Fatalf("%d teleports, snapshots on %v; want her through the forest and back on the town", len(tel), maps)
		}
	})
}
