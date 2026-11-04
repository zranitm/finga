package application_test

import (
	"context"
	"errors"
	"log/slog"
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

func TestWorld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		saver := &fakeSaver{}
		w := application.New(data, saver, slog.New(slog.DiscardHandler), deps(application.Options{}))
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()
		go w.RunPersistence(ctx)

		{ // one instance per map
			for _, m := range data.Maps() {
				inst, err := w.Instance(m.ID)
				if err != nil || inst == nil {
					t.Fatalf("%s: %v", m.ID, err)
				}
			}
			if _, err := w.Instance("nowhere"); !errors.Is(err, model.ErrNoMap) {
				t.Fatalf("unknown map: %v", err)
			}
		}

		{ // instances run independently
			town, _ := w.Instance("town")
			forest, _ := w.Instance("forest")
			a := newPlayer("Ayla", data.Town().PlayerSpawn.X, data.Town().PlayerSpawn.Y)
			fm, _ := data.Map("forest")
			b := newPlayer("Robin", fm.PlayerSpawn.X, fm.PlayerSpawn.Y)
			sa, sb := &sink{}, &sink{}
			if err := town.Join(ctx, a, sa); err != nil {
				t.Fatal(err)
			}
			if err := forest.Join(ctx, b, sb); err != nil {
				t.Fatal(err)
			}
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

// leaveAndRun has a player leave town, then one leave the forest, so the persistence queue holds
// "cp town" then "cp forest"; persist then saves them.
func leaveAndRun(t *testing.T, saver *fakeSaver, persist func(ctx context.Context, w *application.World)) {
	t.Helper()
	data := loadData(t)
	w := application.New(data, saver, slog.New(slog.DiscardHandler), deps(application.Options{CheckpointInterval: time.Hour}))
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- w.Run(ctx) }()
	for _, mapID := range []string{"town", "forest"} {
		inst, _ := w.Instance(mapID)
		m, _ := data.Map(mapID)
		p := newPlayer("Ayla", m.PlayerSpawn.X, m.PlayerSpawn.Y)
		if err := inst.Join(ctx, p, &sink{}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(tickInterval)
		synctest.Wait()
		inst.LeaveConnection(p.ID, "")
		time.Sleep(tickInterval)
		synctest.Wait()
	}
	persist(ctx, w)
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
		leaveAndRun(t, saver, func(ctx context.Context, w *application.World) {
			go w.RunPersistence(ctx)
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

func TestShutdownFlushesWithoutRetrying(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		saver := &fakeSaver{err: errors.New("unavailable")}
		start := time.Now()
		leaveAndRun(t, saver, func(context.Context, *application.World) { start = time.Now() })
		if time.Since(start) > time.Second || len(saver.attemptsSoFar()) != 2 {
			t.Fatalf("flush took %s with attempts %v", time.Since(start), saver.attemptsSoFar())
		}
	})
}

func TestSaverErrorsAreLoggedNotFatal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		saver := &fakeSaver{err: errors.New("db down")}
		w := application.New(data, saver, slog.New(slog.DiscardHandler), deps(application.Options{CheckpointInterval: time.Second}))
		ctx, cancel := context.WithCancel(context.Background())
		go func() { _ = w.Run(ctx) }()
		go w.RunPersistence(ctx)
		town, _ := w.Instance("town")
		if err := town.Join(ctx, newPlayer("Ayla", data.Town().PlayerSpawn.X, data.Town().PlayerSpawn.Y), &sink{}); err != nil {
			t.Fatal(err)
		}
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
	inst := application.NewInstance(data.Town(), data, nil, deps(application.Options{}))
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
		inst := application.NewInstance(data.Town(), data, nil, deps(application.Options{}))
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if err := inst.Join(ctx, newPlayer("Ayla", 0, 0), &sink{}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	})
}

func sim0() sim.Input { return sim.Input{} }
