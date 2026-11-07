package world_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/application/economy"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// runningWorld runs a World in the bubble until the test ends.
func runningWorld(t *testing.T) *testWorld {
	t.Helper()
	w := newWorld(t, &fakeSaver{}, worldtest.Options{CheckpointInterval: time.Hour})
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

func detached(s *worldtest.Sink) int {
	n := 0
	for _, e := range s.All() {
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
		if _, err := w.Join(ctx, ayla.Account, ayla.ID, "c-1", &worldtest.Sink{}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("join: %v", err)
		}
		time.Sleep(3 * worldtest.TickInterval)
		synctest.Wait()
		watcher := &worldtest.Sink{}
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
		w.enter(t, bo, "forest", "c-bo", &worldtest.Sink{})
		ayla, s := at(t, "Ayla", "town"), &worldtest.Sink{}
		w.enter(t, ayla, "town", "c-1", s)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		fillInbox(w, bo.ID, "c-bo")
		moved := at(t, "Ayla", "forest")
		moved.ID, moved.Connection = ayla.ID, "c-1"
		w.Transfer(w.MapActor("town"), moved, s, "forest")
		time.Sleep(time.Second)
		synctest.Wait()
		if snaps := worldtest.Of[model.Snapshot](s); len(snaps) != 2 || snaps[1].MapID != "forest" {
			t.Fatalf("snapshots %+v, want the forest's after the town's", snaps)
		}
	})
}

// A transfer that cannot land detaches the connection, so the gateway joins the player again.
func TestAFailedTransferDetachesTheConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := runningWorld(t)
		ayla, s := at(t, "Ayla", "town"), &worldtest.Sink{}
		w.enter(t, ayla, "town", "c-1", s)
		moved := at(t, "Ayla", "forest")
		moved.ID, moved.Connection, moved.Class = ayla.ID, "c-1", kernel.Class("nobody")
		w.Transfer(w.MapActor("town"), moved, s, "forest")
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
		bo, forest := at(t, "Bo", "forest"), &worldtest.Sink{}
		w.enter(t, bo, "forest", "c-bo", forest)
		ayla, s := at(t, "Ayla", "town"), &worldtest.Sink{}
		w.enter(t, ayla, "town", "c-1", s)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		fillInbox(w, bo.ID, "c-bo")
		moved := at(t, "Ayla", "forest")
		moved.ID, moved.Connection = ayla.ID, "c-1"
		w.Transfer(w.MapActor("town"), moved, s, "forest")
		synctest.Wait()
		w.enter(t, ayla, "town", "c-2", &worldtest.Sink{})
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

// A Leave posted just behind a portal or a respawn follows the player: no ghost stays on the target map.
func TestALeaveRacingAPortalLeavesNoGhost(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		w := runningWorld(t)
		townMap, _ := data.Map("town")
		toForest := portalNamed(t, townMap, "to_forest")
		bo, forest := at(t, "Bo", "forest"), &worldtest.Sink{}
		w.enter(t, bo, "forest", "c-bo", forest)
		ayla := worldtest.NewPlayer("Ayla", toForest.Arrival().X, toForest.Arrival().Y)
		w.enter(t, ayla, "town", "c-ayla", &worldtest.Sink{})
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()

		if err := w.EnterPortal(ayla.ID, "c-ayla"); err != nil {
			t.Fatal(err)
		}
		if err := w.Leave(context.Background(), ayla.ID, "c-ayla"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * worldtest.TickInterval)
		synctest.Wait()
		if present, removed := seenIn(forest, ayla.ID); present > removed {
			t.Fatalf("Ayla is in the forest after her Leave (seen %d, removed %d)", present, removed)
		}
	})
}

type idleJobs struct{}

func (idleJobs) JobStarted()  {}
func (idleJobs) JobFinished() {}

// On shutdown a map can stop while another still runs its last tick; a transfer into it is no failure, as the player was saved there.
func TestATransferIntoAMapThatStoppedFirstIsNoFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		workers := economy.NewWorkers(economy.Deps{Observer: idleJobs{}, Panics: worldtest.DiscardPanics()}, economy.Options{Workers: 1})
		w := newWorld(t, &fakeSaver{}, worldtest.Options{CheckpointInterval: time.Hour, Workers: workers, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = w.Run(ctx) }()
		ayla, s := at(t, "Ayla", "town"), &worldtest.Sink{}
		w.enter(t, ayla, "town", "c-1", s)
		release := make(chan struct{})
		workers.Submit(func(context.Context) { <-release }) // holds the World's Run open after its maps stop
		synctest.Wait()
		cancel()
		synctest.Wait()
		moved := at(t, "Ayla", "forest")
		moved.ID, moved.Connection = ayla.ID, "c-1"
		w.Transfer(w.MapActor("town"), moved, s, "forest")
		synctest.Wait()
		close(release)
		<-done
		if n := detached(s); n != 0 || strings.Contains(logs.String(), "transfer failed") {
			t.Fatalf("%d Detached events, logs %s", n, logs.String())
		}
	})
}

// Mid-portal no map holds the player, so the registry's own record of the connection is what a failed login elsewhere detaches.
func TestAFailedLoginElsewhereMidPortalDetachesTheOlderConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := runningWorld(t)
		bo := at(t, "Bo", "forest")
		w.enter(t, bo, "forest", "c-bo", &worldtest.Sink{})
		ayla, s := at(t, "Ayla", "town"), &worldtest.Sink{}
		w.enter(t, ayla, "town", "c-1", s)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		fillInbox(w, bo.ID, "c-bo") // the transfer waits at the forest
		moved := at(t, "Ayla", "forest")
		moved.ID, moved.Account, moved.Connection = ayla.ID, ayla.Account, "c-1"
		w.Transfer(w.MapActor("town"), moved, s, "forest")
		synctest.Wait()
		delete(w.chars.chars, ayla.ID) // the login elsewhere fails on the load
		if _, err := w.Join(context.Background(), ayla.Account, ayla.ID, "c-2", &worldtest.Sink{}); err == nil {
			t.Fatal("the join succeeded")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if n := detached(s); n != 1 {
			t.Fatalf("%d Detached events for the older connection, want one", n)
		}
	})
}

// Two joins of one character through two connections at once, with no gateway to order them, leave the registry and the map naming the same connection.
func TestTwoJoinsAtOnceLeaveTheRegistryAndTheMapAgreeing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := runningWorld(t)
		watcher := &worldtest.Sink{}
		w.enter(t, at(t, "Bo", "town"), "town", "c-bo", watcher)
		ayla := at(t, "Ayla", "town")
		w.chars.chars[ayla.ID] = model.Character{ID: ayla.ID, Account: ayla.Account, Name: ayla.Name, Class: ayla.Class, Level: 1, Stats: ayla.Stats, HP: ayla.HP, MP: ayla.MP, MapID: "town", X: ayla.X, Y: ayla.Y}
		w.chars.mu.Lock()
		w.chars.slow, w.chars.most = time.Second, 0 // a load slow enough for the other join to start meanwhile
		w.chars.mu.Unlock()
		sinks := map[string]*worldtest.Sink{"c-1": {}, "c-2": {}}
		var joins sync.WaitGroup
		for conn, s := range sinks {
			joins.Go(func() {
				if _, err := w.Join(context.Background(), ayla.Account, ayla.ID, conn, s); err != nil {
					t.Error(err)
				}
			})
		}
		joins.Wait()
		w.chars.mu.Lock()
		most := w.chars.most
		w.chars.mu.Unlock()
		if most != 1 {
			t.Fatalf("%d loads of Ayla at once; the second join must wait for the first's whole join", most)
		}
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		before := map[string]int{"c-1": len(sinks["c-1"].Deltas()), "c-2": len(sinks["c-2"].Deltas())}
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		held, other := "c-1", "c-2" // the map's: the connection still sent the town's deltas
		if len(sinks["c-2"].Deltas()) > before["c-2"] {
			held, other = other, held
		}
		if len(sinks[other].Deltas()) > before[other] || len(sinks[held].Deltas()) == before[held] {
			t.Fatal("the map sends to both connections, or to neither")
		}
		_, removed := seenIn(watcher, ayla.ID)
		if err := w.Leave(context.Background(), ayla.ID, other); err != nil {
			t.Fatal(err)
		}
		if err := w.Leave(context.Background(), ayla.ID, held); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		if _, after := seenIn(watcher, ayla.ID); after != removed+1 {
			t.Fatalf("the town removed Ayla %d more times after both Leaves, want once: the registry named %s's presence", after-removed, other)
		}
	})
}
