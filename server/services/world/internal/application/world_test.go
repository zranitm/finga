package application_test

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
	"finga/services/world/internal/application"
	"finga/services/world/internal/model"
)

// errRefused is a save the character service refuses by rule: never retried.
var errRefused = errors.New("refused")

// fakeSaver records saves; the first failures attempts fail with err.
type fakeSaver struct {
	mu       sync.Mutex
	saved    []model.Checkpoint
	levelUps []model.LevelUpSave
	attempts []string // "cp <map>" or "lu <level>", one per attempt
	err      error
	failures int
}

func (f *fakeSaver) attempt(what string) error {
	f.attempts = append(f.attempts, what)
	if f.err != nil && (f.failures == 0 || len(f.attempts) <= f.failures) {
		return f.err
	}
	return nil
}

func (f *fakeSaver) SaveCheckpoint(_ context.Context, cp model.Checkpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, cp)
	return f.attempt("cp " + cp.MapID)
}

func (f *fakeSaver) SaveLevelUp(_ context.Context, lu model.LevelUpSave) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.levelUps = append(f.levelUps, lu)
	return f.attempt("lu " + string(rune('0'+lu.Level)))
}

func (f *fakeSaver) Retryable(err error) bool { return !errors.Is(err, errRefused) }

func (f *fakeSaver) attemptsSoFar() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.attempts...)
}

func (f *fakeSaver) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.saved)
}

// testWorld is a World whose characters each test saves as it joins them.
type testWorld struct {
	*application.World
	chars *fakeCharacters
}

func newWorld(t *testing.T, saver application.Saver, opts options) *testWorld {
	t.Helper()
	chars := &fakeCharacters{chars: map[kernel.CharacterID]model.Character{}}
	opts.Characters = chars
	return &testWorld{World: newWorldOf(loadData(t), saver, opts), chars: chars}
}

// enter saves p on mapID and joins it there through conn; the World must be running.
func (w *testWorld) enter(t *testing.T, p model.Player, mapID, conn string, s application.Sink) {
	t.Helper()
	if err := w.join(p, mapID, conn, s); err != nil {
		t.Fatal(err)
	}
}

func (w *testWorld) join(p model.Player, mapID, conn string, s application.Sink) error {
	w.chars.chars[p.ID] = model.Character{ID: p.ID, Account: p.Account, Name: p.Name, Class: p.Class, Level: p.Level, Exp: p.Exp, Stats: p.Stats, HP: p.HP, MP: p.MP, MapID: mapID, X: p.X, Y: p.Y}
	_, err := w.Join(context.Background(), p.Account, p.ID, conn, s)
	return err
}

// at is a new player at mapID's spawn.
func at(t *testing.T, name, mapID string) model.Player {
	t.Helper()
	m, ok := loadData(t).Map(mapID)
	if !ok {
		t.Fatalf("no map %s", mapID)
	}
	return newPlayer(name, m.PlayerSpawn.X, m.PlayerSpawn.Y)
}

func TestWorld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		saver := &fakeSaver{}
		w := newWorld(t, saver, options{})
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()

		{ // a character joins the map it was saved on; an unknown map is refused
			if err := w.join(at(t, "Cass", "town"), "nowhere", "c-0", &sink{}); !errors.Is(err, model.ErrNoMap) {
				t.Fatalf("unknown map: %v", err)
			}
		}

		{ // instances run independently
			sa, sb := &sink{}, &sink{}
			w.enter(t, at(t, "Ayla", "town"), "town", "c-1", sa)
			w.enter(t, at(t, "Robin", "forest"), "forest", "c-2", sb)
			time.Sleep(2 * tickInterval)
			synctest.Wait()
			if snap := sa.all()[0].(model.Snapshot); snap.MapID != "town" || len(snap.Players) != 1 {
				t.Fatalf("town snapshot %+v", snap)
			}
			if snap := sb.all()[0].(model.Snapshot); snap.MapID != "forest" || len(snap.Players) != 1 {
				t.Fatalf("forest snapshot %+v", snap)
			}
		}

		{ // checkpoints reach the saver through the worker
			time.Sleep(31 * time.Second)
			synctest.Wait()
			if got := saver.count(); got != 2 {
				t.Fatalf("%d checkpoints saved after 30 s with two players, want 2", got)
			}
		}

		{ // shutdown flushes the remaining checkpoints
			before := saver.count()
			cancel()
			if err := <-runDone; err != nil {
				t.Fatal(err)
			}
			synctest.Wait() // the persistence goroutine exits on cancel
			if got := saver.count() - before; got != 2 {
				t.Fatalf("%d checkpoints flushed on shutdown, want 2 (one per player)", got)
			}
		}
	})
}

// leaveAndRun has a player leave town, then one leave the forest, so the World saves "cp town" then
// "cp forest"; then it runs before and shuts the World down.
func leaveAndRun(t *testing.T, saver *fakeSaver, before func()) {
	t.Helper()
	w := newWorld(t, saver, options{CheckpointInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- w.Run(ctx) }()
	for _, mapID := range []string{"town", "forest"} {
		p := at(t, "Ayla", mapID)
		w.enter(t, p, mapID, "c-1", &sink{})
		time.Sleep(tickInterval)
		synctest.Wait()
		if err := w.Leave(p.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(tickInterval)
		synctest.Wait()
	}
	before()
	cancel()
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
}

// A character service restart must not lose a save, and an older one must land first.
func TestThePersistenceWorkerRetriesInOrder(t *testing.T) {
	run := func(t *testing.T, saver *fakeSaver) []string {
		t.Helper()
		var attempts []string
		leaveAndRun(t, saver, func() {
			time.Sleep(2 * time.Minute)
			synctest.Wait()
			attempts = saver.attemptsSoFar()
		})
		return attempts
	}
	t.Run("a save that fails twice is retried, and the next job waits for it", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			got := run(t, &fakeSaver{err: errors.New("unavailable"), failures: 2})
			if len(got) != 4 || got[0] != "cp town" || got[2] != "cp town" || got[3] != "cp forest" {
				t.Fatalf("attempts %v", got)
			}
		})
	})
	t.Run("one that keeps failing is given up after the limit, and the worker goes on", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			got := run(t, &fakeSaver{err: errors.New("unavailable")})
			first := 0
			for _, a := range got {
				if a == "cp town" {
					first++
				}
			}
			// About fifteen tries fit in a minute; then the first save is dropped and the next tried.
			if first < 3 || first > 20 || got[first] != "cp forest" {
				t.Fatalf("%d attempts at the first save, then %v", first, got[first:])
			}
		})
	})
	t.Run("a refused save is not retried", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			if got := run(t, &fakeSaver{err: errRefused}); len(got) != 2 {
				t.Fatalf("attempts %v", got)
			}
		})
	})
}

// At shutdown each save still waiting, the one being retried included, gets one last try and no more.
func TestShutdownTriesEachSaveOnceMore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		saver := &fakeSaver{err: errors.New("unavailable")}
		var start time.Time
		before := 0
		leaveAndRun(t, saver, func() { start, before = time.Now(), len(saver.attemptsSoFar()) })
		after := saver.attemptsSoFar()[before:]
		if time.Since(start) > time.Second || len(after) != 2 || after[0] != "cp town" || after[1] != "cp forest" {
			t.Fatalf("shutdown took %s with attempts %v after it began", time.Since(start), after)
		}
	})
}

func TestSaverErrorsAreLoggedNotFatal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		saver := &fakeSaver{err: errors.New("db down")}
		w := newWorld(t, saver, options{CheckpointInterval: time.Second})
		ctx, cancel := context.WithCancel(context.Background())
		go func() { _ = w.Run(ctx) }()
		w.enter(t, at(t, "Ayla", "town"), "town", "c-1", &sink{})
		time.Sleep(3 * time.Second)
		synctest.Wait()
		if saver.count() < 2 {
			t.Fatalf("%d attempts; the worker should keep trying every interval", saver.count())
		}
		cancel()
		synctest.Wait()
	})
}

func TestJoinWhenTheInboxIsFull(t *testing.T) {
	data := loadData(t)
	inst := newInstance(data.Town(), data, nil, options{})
	// Not running: 4096 posts fill the inbox, the next is refused.
	for seq := range uint32(4096) {
		if !inst.Input(kernel.NewCharacterID(), seq, sim0()) {
			t.Fatalf("post %d refused", seq)
		}
	}
	if inst.Input(kernel.NewCharacterID(), 1, sim0()) {
		t.Fatal("a post accepted with a full inbox")
	}
	if err := inst.Join(context.Background(), newPlayer("Ayla", 0, 0), &sink{}); !errors.Is(err, model.ErrBusy) {
		t.Fatalf("join with a full inbox: %v", err)
	}
	if inst.LeaveConnection(kernel.NewCharacterID(), "") {
		t.Fatal("leave accepted with a full inbox")
	}
	t.Run("a join on a stopped instance gives up when the caller's context ends", func(t *testing.T) {
		inst := newInstance(data.Town(), data, nil, options{})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if err := inst.Join(ctx, newPlayer("Ayla", 0, 0), &sink{}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	})
}

func sim0() sim.Input { return sim.Input{} }

// A join through a newer connection on another map takes the character off the old one.
func TestAJoinElsewhereRemovesTheOlderPresence(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld(t, &fakeSaver{}, options{CheckpointInterval: time.Hour})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = w.Run(ctx) }()
		ayla, bo := at(t, "Ayla", "town"), at(t, "Bo", "town")
		watcher := &sink{}
		w.enter(t, bo, "town", "c-bo", watcher)
		w.enter(t, ayla, "town", "c-1", &sink{})
		time.Sleep(tickInterval)
		synctest.Wait()

		forest := &sink{}
		there := at(t, "Ayla", "forest")
		there.ID = ayla.ID
		w.enter(t, there, "forest", "c-2", forest)
		time.Sleep(2 * tickInterval)
		synctest.Wait()
		if _, removed := seenIn(watcher, ayla.ID); removed != 1 {
			t.Fatalf("the town removed her %d times, want once", removed)
		}
		if snaps := snapshots(forest); len(snaps) != 1 || snaps[0].MapID != "forest" {
			t.Fatalf("forest snapshots %+v", snaps)
		}
		if err := w.Leave(ayla.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * tickInterval)
		synctest.Wait()
		if _, removed := seenIn(forest, ayla.ID); removed != 0 {
			t.Fatal("the older connection's Leave removed her from the forest")
		}
	})
}

// A portal taken by a presence a newer join replaced moves nobody.
func TestATransferOfAReplacedPresenceIsDropped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		w := newWorld(t, &fakeSaver{}, options{CheckpointInterval: time.Hour})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = w.Run(ctx) }()
		townMap, _ := data.Map("town")
		toForest := portalNamed(t, townMap, "to_forest")
		ayla := newPlayer("Ayla", toForest.Arrival().X, toForest.Arrival().Y)
		old := &sink{}
		w.enter(t, ayla, "town", "c-1", old)
		time.Sleep(tickInterval)
		synctest.Wait()

		// The old connection takes the portal while the newer one joins the forest, before the town's tick.
		if err := w.EnterPortal(ayla.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		newer := &sink{}
		there := at(t, "Ayla", "forest")
		there.ID = ayla.ID
		w.enter(t, there, "forest", "c-2", newer)
		time.Sleep(3 * tickInterval)
		synctest.Wait()
		if snaps := snapshots(old); len(snaps) != 1 {
			t.Fatalf("the old connection got %d snapshots: its presence was moved", len(snaps))
		}
		before := len(newer.deltas())
		time.Sleep(2 * tickInterval)
		synctest.Wait()
		if len(newer.deltas()) == before {
			t.Fatal("the newer connection stopped receiving deltas: the stale transfer replaced it")
		}
	})
}

func TestAFailedSaveIsLoggedWithItsJobAndCharacter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		w := newWorld(t, &fakeSaver{err: errRefused}, options{CheckpointInterval: time.Hour, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()
		p := at(t, "Ayla", "town")
		w.enter(t, p, "town", "c-1", &sink{})
		time.Sleep(tickInterval)
		synctest.Wait()
		if err := w.Leave(p.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(tickInterval)
		synctest.Wait()
		cancel()
		if err := <-runDone; err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"msg":"persistence failed"`, `"job":"checkpoint"`, `"character_id":"` + p.ID.String() + `"`, `"map_id":"town"`} {
			if !strings.Contains(logs.String(), want) {
				t.Fatalf("log %q lacks %s", logs.String(), want)
			}
		}
	})
}
