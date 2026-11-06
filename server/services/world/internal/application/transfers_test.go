package application_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/model"
)

// runningWorld runs a World in the bubble until the test ends.
func runningWorld(t *testing.T) *testWorld {
	t.Helper()
	w := newWorld(t, &fakeSaver{}, options{CheckpointInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return w
}

// fillInbox fills the map's inbox through a player on it, so the next post there is refused.
func fillInbox(w *testWorld, id kernel.CharacterID, conn string) {
	for seq := uint32(1); w.Input(id, conn, seq, sim.Input{}) == nil; seq++ {
	}
}

func detached(s *sink) int {
	n := 0
	for _, e := range s.all() {
		if _, ok := e.(model.Detached); ok {
			n++
		}
	}
	return n
}

// A join whose caller stopped waiting after it was posted still lands; the world takes it back.
func TestAJoinItsCallerGaveUpOnLeavesNoGhost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := runningWorld(t)
		ayla := at(t, "Ayla", "town")
		w.chars.chars[ayla.ID] = model.Character{ID: ayla.ID, Account: ayla.Account, Name: ayla.Name, Class: ayla.Class, Level: 1, Stats: ayla.Stats, HP: ayla.HP, MP: ayla.MP, MapID: "town", X: ayla.X, Y: ayla.Y}
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		defer cancel()
		if _, err := w.Join(ctx, ayla.Account, ayla.ID, "c-1", &sink{}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("join: %v", err)
		}
		time.Sleep(3 * tickInterval)
		synctest.Wait()
		watcher := &sink{}
		w.enter(t, at(t, "Bo", "town"), "town", "c-bo", watcher)
		if present, _ := seenIn(watcher, ayla.ID); present != 0 {
			t.Fatal("the join nobody waited for left Ayla on the map")
		}
	})
}

// A transfer to a map whose inbox is full waits for it, rather than leaving the player nowhere.
func TestATransferRetriesWhileTheMapIsBusy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := runningWorld(t)
		bo := at(t, "Bo", "forest")
		w.enter(t, bo, "forest", "c-bo", &sink{})
		ayla, s := at(t, "Ayla", "town"), &sink{}
		w.enter(t, ayla, "town", "c-1", s)
		time.Sleep(tickInterval)
		synctest.Wait()
		fillInbox(w, bo.ID, "c-bo")
		moved := at(t, "Ayla", "forest")
		moved.ID, moved.Connection = ayla.ID, "c-1"
		w.Transfer(moved, s, "forest")
		time.Sleep(time.Second)
		synctest.Wait()
		if snaps := snapshots(s); len(snaps) != 2 || snaps[1].MapID != "forest" {
			t.Fatalf("snapshots %+v, want the forest's after the town's", snaps)
		}
	})
}

// A transfer that cannot land detaches the connection, so the gateway joins the player again.
func TestAFailedTransferDetachesTheConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := runningWorld(t)
		ayla, s := at(t, "Ayla", "town"), &sink{}
		w.enter(t, ayla, "town", "c-1", s)
		moved := at(t, "Ayla", "forest")
		moved.ID, moved.Connection, moved.Class = ayla.ID, "c-1", kernel.Class("nobody")
		w.Transfer(moved, s, "forest")
		time.Sleep(time.Second)
		synctest.Wait()
		if n := detached(s); n != 1 {
			t.Fatalf("%d Detached events, want one", n)
		}
	})
}

// A transfer still waiting when a newer connection joins the character elsewhere never lands.
func TestATransferStopsOnceANewerJoinTakesTheCharacter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := runningWorld(t)
		bo, forest := at(t, "Bo", "forest"), &sink{}
		w.enter(t, bo, "forest", "c-bo", forest)
		ayla, s := at(t, "Ayla", "town"), &sink{}
		w.enter(t, ayla, "town", "c-1", s)
		time.Sleep(tickInterval)
		synctest.Wait()
		fillInbox(w, bo.ID, "c-bo")
		moved := at(t, "Ayla", "forest")
		moved.ID, moved.Connection = ayla.ID, "c-1"
		w.Transfer(moved, s, "forest")
		synctest.Wait()
		w.enter(t, ayla, "town", "c-2", &sink{})
		time.Sleep(time.Second)
		synctest.Wait()
		if present, _ := seenIn(forest, ayla.ID); present != 0 {
			t.Fatal("the stale transfer put Ayla in the forest")
		}
		if n := detached(s); n != 0 {
			t.Fatalf("%d Detached events for a connection that was replaced", n)
		}
	})
}

// A Leave posted just behind a portal or a respawn follows the player: the transfer never lands, and no
// ghost stays on the target map.
func TestALeaveRacingAPortalLeavesNoGhost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		w := runningWorld(t)
		townMap, _ := data.Map("town")
		toForest := portalNamed(t, townMap, "to_forest")
		bo, forest := at(t, "Bo", "forest"), &sink{}
		w.enter(t, bo, "forest", "c-bo", forest)
		ayla := newPlayer("Ayla", toForest.Arrival().X, toForest.Arrival().Y)
		w.enter(t, ayla, "town", "c-ayla", &sink{})
		time.Sleep(tickInterval)
		synctest.Wait()

		if err := w.EnterPortal(ayla.ID, "c-ayla"); err != nil {
			t.Fatal(err)
		}
		if err := w.Leave(context.Background(), ayla.ID, "c-ayla"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * tickInterval)
		synctest.Wait()
		if present, removed := seenIn(forest, ayla.ID); present > removed {
			t.Fatalf("Ayla is in the forest after her Leave (seen %d, removed %d)", present, removed)
		}
	})
}
