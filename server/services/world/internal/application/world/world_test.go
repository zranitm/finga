package world_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/application/instance"
	"finga/services/world/internal/application/world"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// errRefused is a save the character service refuses by rule: never retried.
var errRefused = errors.New("refused")

// errUnavailable is a transient failure.
var errUnavailable = errors.New("unavailable")

// errUnexplained is a failure without a reason, such as Internal: retried a while, then dropped.
var errUnexplained = errors.New("internal")

// fakeSaver records saves; the first failures attempts fail with err.
type fakeSaver struct {
	mu       sync.Mutex
	saved    []model.Checkpoint
	attempts []string // "cp <map>", one per attempt
	err      error
	failures int
	hangs    int           // the first hangs attempts wait until their context ends
	slow     time.Duration // every attempt takes this long
	failing  string        // a map whose saves always fail with err, whatever failures says
	dropped  []string      // why each save the World gave up on was dropped
}

// attempt records what is tried; f.mu is held and attempt unlocks it.
func (f *fakeSaver) attempt(ctx context.Context, what string) error {
	f.attempts = append(f.attempts, what)
	n, err := len(f.attempts), f.err
	f.mu.Unlock()
	if n <= f.hangs {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.slow > 0 {
		select {
		case <-time.After(f.slow):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	fails := f.failures == 0 || n <= f.failures
	if f.failing != "" {
		fails = what == "cp "+f.failing
	}
	if err != nil && fails {
		return err
	}
	return nil
}

func (f *fakeSaver) SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error {
	f.mu.Lock()
	f.saved = append(f.saved, cp)
	return f.attempt(ctx, "cp "+cp.MapID)
}

func (f *fakeSaver) Retryable(err error) bool {
	return !errors.Is(err, errRefused) && !errors.Is(err, errUnexplained)
}

func (f *fakeSaver) Refused(err error) bool { return errors.Is(err, errRefused) }

// SaveDropped records why the World gave up on a save.
func (f *fakeSaver) SaveDropped(reason string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dropped = append(f.dropped, reason)
}

func (f *fakeSaver) droppedSoFar() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.dropped...)
}

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
	*world.World
	chars *fakeCharacters
}

func newWorld(t *testing.T, saver world.Saver, opts worldtest.Options) *testWorld {
	t.Helper()
	chars := &fakeCharacters{chars: map[kernel.CharacterID]model.Character{}}
	opts.Characters = chars
	return &testWorld{World: worldtest.NewWorld(worldtest.LoadData(t), saver, opts), chars: chars}
}

// enter saves p on mapID and joins it there through conn; the World must be running.
func (w *testWorld) enter(t *testing.T, p model.Player, mapID, conn string, s instance.Sink) {
	t.Helper()
	if err := w.join(p, mapID, conn, s); err != nil {
		t.Fatal(err)
	}
}

func (w *testWorld) join(p model.Player, mapID, conn string, s instance.Sink) error {
	w.chars.chars[p.ID] = model.Character{ID: p.ID, Account: p.Account, Name: p.Name, Class: p.Class, Level: p.Level, Exp: p.Exp, Points: p.Points, HP: p.HP, MP: p.MP, MapID: mapID, X: p.X, Y: p.Y}
	_, err := w.Join(context.Background(), p.Account, p.ID, conn, s)
	return err
}

// at is a new player at mapID's spawn.
func at(t *testing.T, name, mapID string) model.Player {
	t.Helper()
	m, ok := worldtest.LoadData(t).Map(mapID)
	if !ok {
		t.Fatalf("no map %s", mapID)
	}
	return worldtest.NewPlayer(name, m.PlayerSpawn.X, m.PlayerSpawn.Y)
}

func TestWorld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		saver := &fakeSaver{}
		w := newWorld(t, saver, worldtest.Options{})
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()

		{ // a character joins the map it was saved on; an unknown map is refused
			if err := w.join(at(t, "Cass", "town"), "nowhere", "c-0", &worldtest.Sink{}); !errors.Is(err, model.ErrNoMap) {
				t.Fatalf("unknown map: %v", err)
			}
		}

		{ // instances run independently
			sa, sb := &worldtest.Sink{}, &worldtest.Sink{}
			w.enter(t, at(t, "Ayla", "town"), "town", "c-1", sa)
			w.enter(t, at(t, "Robin", "forest"), "forest", "c-2", sb)
			time.Sleep(2 * worldtest.TickInterval)
			synctest.Wait()
			if snap := sa.All()[0].(model.Snapshot); snap.MapID != "town" || len(snap.Players) != 1 {
				t.Fatalf("town snapshot %+v", snap)
			}
			if snap := sb.All()[0].(model.Snapshot); snap.MapID != "forest" || len(snap.Players) != 1 {
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

// leaveAndRun has the World save "cp town" then "cp forest", then runs before and shuts the World down.
func leaveAndRun(t *testing.T, saver *fakeSaver, before func()) {
	t.Helper()
	w := newWorld(t, saver, worldtest.Options{CheckpointInterval: time.Hour, SaveObserver: saver})
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- w.Run(ctx) }()
	for _, mapID := range []string{"town", "forest"} {
		p := at(t, "Ayla", mapID)
		w.enter(t, p, mapID, "c-1", &worldtest.Sink{})
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		if err := w.Leave(context.Background(), p.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
	}
	before()
	cancel()
	if err := <-runDone; err != nil {
		t.Fatal(err)
	}
}

// count is how many of attempts are what.
func count(attempts []string, what string) int {
	n := 0
	for _, a := range attempts {
		if a == what {
			n++
		}
	}
	return n
}

// A failed save is retried behind the others until it lands, so a character service outage loses no level-up.
func TestThePersistenceWorkerRetriesBehindTheOthers(t *testing.T) {
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
	t.Run("the first two attempts fail, and each save then lands once", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			got := run(t, &fakeSaver{err: errUnavailable, failures: 2})
			if len(got) != 4 || count(got[2:], "cp town") != 1 || count(got[2:], "cp forest") != 1 {
				t.Fatalf("attempts %v", got)
			}
		})
	})
	t.Run("a save the character service never answers is cut off, and the next goes first", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			got := run(t, &fakeSaver{hangs: 1})
			if len(got) != 3 || got[0] != "cp town" || got[1] != "cp forest" || got[2] != "cp town" {
				t.Fatalf("attempts %v", got)
			}
		})
	})
	t.Run("one that keeps failing is still tried after a minute, and holds up no other", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			saver := &fakeSaver{err: errUnavailable, failing: "town"}
			var atOne, atTwo []string
			leaveAndRun(t, saver, func() {
				time.Sleep(time.Minute)
				synctest.Wait()
				atOne = saver.attemptsSoFar()
				time.Sleep(time.Minute)
				synctest.Wait()
				atTwo = saver.attemptsSoFar()
			})
			if count(atOne, "cp forest") != 1 || count(atTwo, "cp town") <= count(atOne, "cp town") {
				t.Fatalf("attempts after a minute %v, after two %v", atOne, atTwo)
			}
		})
	})
	t.Run("a refused save is not retried, and is counted dropped", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			saver := &fakeSaver{err: errRefused}
			if got := run(t, saver); len(got) != 2 || !slices.Equal(saver.droppedSoFar(), []string{"refused", "refused"}) {
				t.Fatalf("attempts %v, dropped %v", got, saver.droppedSoFar())
			}
		})
	})
	t.Run("one failing without a reason is retried for a minute, then dropped and counted", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			saver := &fakeSaver{err: errUnexplained, failing: "town"}
			var early, late []string
			leaveAndRun(t, saver, func() {
				time.Sleep(30 * time.Second)
				synctest.Wait()
				early = saver.attemptsSoFar()
				time.Sleep(90 * time.Second)
				synctest.Wait()
				late = saver.attemptsSoFar()
			})
			if count(early, "cp town") < 3 {
				t.Fatalf("attempts after 30 s %v; want it retried", early)
			}
			if final := saver.attemptsSoFar(); count(final, "cp town") != count(late, "cp town") || count(final, "cp forest") != 1 {
				t.Fatalf("attempts %v; want town dropped before the shutdown and forest saved", final)
			}
			if got := saver.droppedSoFar(); !slices.Equal(got, []string{"unexplained"}) {
				t.Fatalf("dropped %v", got)
			}
		})
	})
}

// At shutdown each save still waiting, the one being retried included, gets one last try and no more.
func TestShutdownTriesEachSaveOnceMore(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		saver := &fakeSaver{err: errUnavailable}
		var start time.Time
		before := 0
		leaveAndRun(t, saver, func() { start, before = time.Now(), len(saver.attemptsSoFar()) })
		after := saver.attemptsSoFar()[before:]
		if time.Since(start) > time.Second || len(after) != 2 || count(after, "cp town") != 1 || count(after, "cp forest") != 1 {
			t.Fatalf("shutdown took %s with attempts %v after it began", time.Since(start), after)
		}
		if got := saver.droppedSoFar(); !slices.Equal(got, []string{"shutdown", "shutdown"}) {
			t.Fatalf("dropped %v; want both last tries counted", got)
		}
	})
}

// The last saves share the flush budget: a character service that never answers can't hold the shutdown past it.
func TestShutdownSavesWithinTheFlushBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		w := newWorld(t, &fakeSaver{hangs: 100}, worldtest.Options{CheckpointInterval: time.Hour, FlushTimeout: 7 * time.Second, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()
		w.enter(t, at(t, "Ayla", "town"), "town", "c-1", &worldtest.Sink{})
		w.enter(t, at(t, "Bo", "forest"), "forest", "c-2", &worldtest.Sink{})
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		start := time.Now()
		cancel()
		if err := <-runDone; err != nil {
			t.Fatal(err)
		}
		// Both saves give up at their call timeout (5 s), within the budget.
		if took := time.Since(start); took > 7*time.Second+worldtest.TickInterval {
			t.Fatalf("shutdown took %s", took)
		}
		if n := strings.Count(logs.String(), `"msg":"persistence failed"`); n != 2 {
			t.Fatalf("%d lost saves logged: %s", n, logs.String())
		}
	})
}

// A slow character service costs the shutdown one save's time per batch of players, not per player.
func TestShutdownSavesInParallel(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		w := newWorld(t, &fakeSaver{slow: time.Second}, worldtest.Options{CheckpointInterval: time.Hour, FlushTimeout: 3 * time.Second, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()
		for n := range 10 {
			w.enter(t, at(t, fmt.Sprintf("P%d", n), "town"), "town", fmt.Sprintf("c-%d", n), &worldtest.Sink{})
		}
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		start := time.Now()
		cancel()
		if err := <-runDone; err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took > 2*time.Second+worldtest.TickInterval {
			t.Fatalf("shutdown took %s", took)
		}
		if strings.Contains(logs.String(), `"msg":"persistence failed"`) {
			t.Fatalf("saves lost: %s", logs.String())
		}
	})
}

func TestSaverErrorsAreLoggedNotFatal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		saver := &fakeSaver{err: errUnavailable}
		w := newWorld(t, saver, worldtest.Options{CheckpointInterval: time.Second})
		ctx, cancel := context.WithCancel(context.Background())
		go func() { _ = w.Run(ctx) }()
		w.enter(t, at(t, "Ayla", "town"), "town", "c-1", &worldtest.Sink{})
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
	data := worldtest.LoadData(t)
	inst := worldtest.NewInstance(data.StartTown(), data, nil, worldtest.Options{})
	// Not running: 4096 posts fill the inbox, the next is refused.
	for seq := range uint32(4096) {
		if err := inst.Input(kernel.NewCharacterID(), seq, sim0()); err != nil {
			t.Fatalf("post %d refused", seq)
		}
	}
	if err := inst.Input(kernel.NewCharacterID(), 1, sim0()); !errors.Is(err, model.ErrBusy) {
		t.Fatal("a post accepted with a full inbox")
	}
	if err := inst.Join(context.Background(), worldtest.NewPlayer("Ayla", 0, 0), &worldtest.Sink{}); !errors.Is(err, model.ErrBusy) {
		t.Fatalf("join with a full inbox: %v", err)
	}
	if err := inst.Leave(kernel.NewCharacterID(), "", nil); !errors.Is(err, model.ErrBusy) {
		t.Fatal("leave accepted with a full inbox")
	}
	t.Run("a join on a stopped instance gives up when the caller's context ends", func(t *testing.T) {
		inst := worldtest.NewInstance(data.StartTown(), data, nil, worldtest.Options{})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if err := inst.Join(ctx, worldtest.NewPlayer("Ayla", 0, 0), &worldtest.Sink{}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	})
}

func sim0() sim.Input { return sim.Input{} }

// A join through a newer connection takes the character over: the older one gets nothing, and its Leave is a no-op.
func TestAJoinThroughANewerConnectionReplacesTheOlder(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld(t, &fakeSaver{}, worldtest.Options{CheckpointInterval: time.Hour})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = w.Run(ctx) }()
		ayla, bo := at(t, "Ayla", "town"), at(t, "Bo", "town")
		watcher, old := &worldtest.Sink{}, &worldtest.Sink{}
		w.enter(t, bo, "town", "c-bo", watcher)
		w.enter(t, ayla, "town", "c-1", old)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()

		newer := &worldtest.Sink{}
		if _, err := w.Join(context.Background(), ayla.Account, ayla.ID, "c-2", newer); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		if _, removed := seenIn(watcher, ayla.ID); removed != 1 {
			t.Fatalf("the town removed her %d times, want once", removed)
		}
		if snaps := worldtest.Of[model.Snapshot](newer); len(snaps) != 1 || snaps[0].MapID != "town" {
			t.Fatalf("snapshots %+v", snaps)
		}
		before := len(old.Deltas())
		if err := w.Leave(context.Background(), ayla.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		if _, removed := seenIn(newer, ayla.ID); removed != 0 {
			t.Fatal("the older connection's Leave removed her")
		}
		if len(old.Deltas()) != before {
			t.Fatal("the older connection still gets deltas")
		}
	})
}

// A portal taken by a presence a newer join replaced moves nobody.
func TestATransferOfAReplacedPresenceIsDropped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		w := newWorld(t, &fakeSaver{}, worldtest.Options{CheckpointInterval: time.Hour})
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _ = w.Run(ctx) }()
		townMap, _ := data.Map("town")
		toForest := portalNamed(t, townMap, "to_forest")
		ayla := worldtest.NewPlayer("Ayla", toForest.Arrival().X, toForest.Arrival().Y)
		old := &worldtest.Sink{}
		w.enter(t, ayla, "town", "c-1", old)
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()

		// The old connection takes the portal while the newer one joins the forest, before the town's tick.
		if err := w.EnterPortal(ayla.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		newer := &worldtest.Sink{}
		there := at(t, "Ayla", "forest")
		there.ID = ayla.ID
		w.enter(t, there, "forest", "c-2", newer)
		time.Sleep(3 * worldtest.TickInterval)
		synctest.Wait()
		if snaps := worldtest.Of[model.Snapshot](old); len(snaps) != 1 {
			t.Fatalf("the old connection got %d snapshots: its presence was moved", len(snaps))
		}
		before := len(newer.Deltas())
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		if len(newer.Deltas()) == before {
			t.Fatal("the newer connection stopped receiving deltas: the stale transfer replaced it")
		}
	})
}

func TestARefusedSaveIsLoggedWithItsCharacter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		w := newWorld(t, &fakeSaver{err: errRefused}, worldtest.Options{CheckpointInterval: time.Hour, Logger: slog.New(slog.NewJSONHandler(&logs, nil))})
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()
		p := at(t, "Ayla", "town")
		w.enter(t, p, "town", "c-1", &worldtest.Sink{})
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		if err := w.Leave(context.Background(), p.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		time.Sleep(worldtest.TickInterval)
		synctest.Wait()
		cancel()
		if err := <-runDone; err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{`"msg":"persistence failed"`, `"character_id":"` + p.ID.String() + `"`, `"map_id":"town"`} {
			if !strings.Contains(logs.String(), want) {
				t.Fatalf("log %q lacks %s", logs.String(), want)
			}
		}
	})
}

// Once its maps have stopped, joins, leaves and commands are ErrStopped at once, not a wait for the caller's deadline.
func TestAStoppedWorldAnswersStopped(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		w := newWorld(t, &fakeSaver{}, worldtest.Options{CheckpointInterval: time.Hour})
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()
		ayla := at(t, "Ayla", "town")
		w.enter(t, ayla, "town", "c-1", &worldtest.Sink{})
		cancel()
		if err := <-runDone; err != nil {
			t.Fatal(err)
		}
		longCtx, stop := context.WithTimeout(context.Background(), time.Hour)
		defer stop()
		start := time.Now()
		if err := w.Input(ayla.ID, "c-1", 1, sim.Input{}); !errors.Is(err, model.ErrStopped) {
			t.Fatalf("input: %v", err)
		}
		if err := w.Leave(longCtx, ayla.ID, "c-1"); !errors.Is(err, model.ErrStopped) {
			t.Fatalf("leave: %v", err)
		}
		bo := at(t, "Bo", "town")
		w.chars.chars[bo.ID] = model.Character{ID: bo.ID, Account: bo.Account, Name: bo.Name, Class: bo.Class, Level: 1, Points: bo.Points, HP: bo.HP, MP: bo.MP, MapID: "town", X: bo.X, Y: bo.Y}
		if _, err := w.Join(longCtx, bo.Account, bo.ID, "c-2", &worldtest.Sink{}); !errors.Is(err, model.ErrStopped) {
			t.Fatalf("join: %v", err)
		}
		if took := time.Since(start); took > time.Second {
			t.Fatalf("answering took %s", took)
		}
	})
}
