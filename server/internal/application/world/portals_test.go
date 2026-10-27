package world_test

import (
	"context"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/character"
	"finga/internal/application/world"
	"finga/internal/gamedata"
	"finga/internal/model"
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

func teleports(s *sink) []world.Teleport {
	var out []world.Teleport
	for _, e := range s.all() {
		if tp, ok := e.(world.Teleport); ok {
			out = append(out, tp)
		}
	}
	return out
}

func snapshots(s *sink) []world.Snapshot {
	var out []world.Snapshot
	for _, e := range s.all() {
		if sn, ok := e.(world.Snapshot); ok {
			out = append(out, sn)
		}
	}
	return out
}

// seenIn counts the deltas and snapshots of s that carry id, and the
// deltas that remove it.
func seenIn(s *sink, id model.CharacterID) (present, removed int) {
	for _, e := range s.all() {
		switch ev := e.(type) {
		case world.Snapshot:
			for _, v := range ev.Players {
				if v.ID == id {
					present++
				}
			}
		case world.Delta:
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
		}
	}
	return present, removed
}

func TestPortals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		saver := &fakeSaver{}
		w := world.New(data, saver, slog.New(slog.DiscardHandler), world.Options{TickInterval: tickInterval, CheckpointInterval: time.Hour})
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()
		go w.RunPersistence(ctx)
		defer func() {
			cancel()
			if err := <-runDone; err != nil {
				t.Error(err)
			}
			synctest.Wait()
		}()
		town, _ := w.Instance("town")
		forest, _ := w.Instance("forest")
		townMap, _ := data.Map("town")
		forestMap, _ := data.Map("forest")
		toForest := portalNamed(t, townMap, "to_forest")
		fromTown := portalNamed(t, forestMap, "from_town")
		arrival := fromTown.Arrival()
		time.Sleep(tickInterval)
		synctest.Wait()

		// Robin stays in the town and watches.
		robin := newPlayer("Robin", townMap.PlayerSpawn.X, townMap.PlayerSpawn.Y)
		sr := &sink{}
		if err := town.Join(ctx, robin, sr); err != nil {
			t.Fatal(err)
		}

		{ // a portal takes the player to the linked spot on the target map, with HP as it was
			ayla := newPlayer("Ayla", toForest.Arrival().X, toForest.Arrival().Y)
			ayla.HP = 150
			sa := &sink{}
			if err := town.Join(ctx, ayla, sa); err != nil {
				t.Fatal(err)
			}
			time.Sleep(tickInterval)
			synctest.Wait()
			town.EnterPortal(ayla.ID)
			time.Sleep(2 * tickInterval)
			synctest.Wait()
			tel := teleports(sa)
			if len(tel) != 1 || tel[0].MapID != "forest" || tel[0].X != arrival.X || tel[0].Y != arrival.Y {
				t.Fatalf("teleports %+v, want one to the forest at %+v", tel, arrival)
			}
			snaps := snapshots(sa)
			if len(snaps) != 2 || snaps[1].MapID != "forest" {
				t.Fatalf("snapshots %+v", snaps)
			}
			var me world.PlayerView
			for _, v := range snaps[1].Players {
				if v.ID == ayla.ID {
					me = v
				}
			}
			if me.ID != ayla.ID || me.State.X != arrival.X || me.State.Y != arrival.Y || me.HP != 150 {
				t.Fatalf("in the forest snapshot: %+v", me)
			}
			// Never in no instance for more than one tick: the forest's
			// snapshot came by the tick after the town's teleport.
			if snaps[1].Tick > tel[0].Tick+1 {
				t.Fatalf("left the town at tick %d, joined the forest at tick %d", tel[0].Tick, snaps[1].Tick)
			}
			// The town checkpointed her as already in the forest.
			synctest.Wait()
			saver.mu.Lock()
			var cp *character.Checkpoint
			for k := range saver.saved {
				if saver.saved[k].ID == ayla.ID {
					cp = &saver.saved[k]
				}
			}
			saver.mu.Unlock()
			if cp == nil || cp.MapID != "forest" || cp.X != arrival.X || cp.HP != 150 {
				t.Fatalf("checkpoint on the way out: %+v", cp)
			}
			// Robin saw her leave exactly once and never again.
			if present, removed := seenIn(sr, ayla.ID); removed != 1 || present == 0 {
				t.Fatalf("robin saw ayla %d times and removed %d times", present, removed)
			}
			// Spamming the portal she just left changes nothing more.
			for range 20 {
				town.EnterPortal(ayla.ID)
			}
			time.Sleep(2 * tickInterval)
			synctest.Wait()
			if len(teleports(sa)) != 1 {
				t.Fatal("a second teleport after the player had left")
			}
		}

		{ // off a portal, EnterPortal does nothing
			before := len(sr.all())
			town.EnterPortal(robin.ID)
			time.Sleep(2 * tickInterval)
			synctest.Wait()
			if len(teleports(sr)) != 0 {
				t.Fatal("teleported from the town spawn")
			}
			if v, ok := sr.own(robin.ID); ok && v.State.X != townMap.PlayerSpawn.X {
				t.Fatalf("moved: %+v", v)
			}
			if len(sr.all()) <= before {
				t.Fatal("robin stopped receiving deltas")
			}
		}

		{ // spamming EnterPortal: one hop, present in exactly one instance at a time
			cass := newPlayer("Cass", toForest.Arrival().X, toForest.Arrival().Y)
			sc := &sink{}
			if err := town.Join(ctx, cass, sc); err != nil {
				t.Fatal(err)
			}
			time.Sleep(tickInterval)
			synctest.Wait()
			for range 50 {
				town.EnterPortal(cass.ID)
				forest.EnterPortal(cass.ID) // arrives before the join, so it is ignored
			}
			time.Sleep(3 * tickInterval)
			synctest.Wait()
			tel := teleports(sc)
			snaps := snapshots(sc)
			if len(tel) != 1 || len(snaps) != 2 || snaps[1].MapID != "forest" || snaps[1].Tick > tel[0].Tick+1 {
				t.Fatalf("teleports %+v snapshots %d", tel, len(snaps))
			}
			if _, removed := seenIn(sr, cass.ID); removed != 1 {
				t.Fatalf("the town removed cass %d times", removed)
			}
			// Every delta after the teleport came from the forest only: the
			// town never sent her another one.
			for _, e := range sc.all() {
				if d, ok := e.(world.Delta); ok && d.Tick > tel[0].Tick+1 {
					for _, r := range d.Removed {
						if r == cass.ID {
							t.Fatal("cass was removed from a map she was on")
						}
					}
				}
			}
			// Standing on the forest's arrival portal, one more EnterPortal
			// takes her back: the handoff works both ways.
			forest.EnterPortal(cass.ID)
			time.Sleep(3 * tickInterval)
			synctest.Wait()
			tel = teleports(sc)
			if len(tel) != 2 || tel[1].MapID != "town" || tel[1].X != toForest.Arrival().X {
				t.Fatalf("teleports %+v", tel)
			}
			if sn := snapshots(sc); len(sn) != 3 || sn[2].MapID != "town" {
				t.Fatalf("snapshots %d", len(sn))
			}
		}
	})
}
