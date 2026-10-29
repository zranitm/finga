package world_test

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
	"finga/services/world/internal/world"
)

// errRefused is a save the character service refuses by rule: never retried.
var errRefused = errors.New("refused")

// fakeSaver records checkpoints and level-ups, as the character service
// would; failures fail the first that many attempts with err.
type fakeSaver struct {
	mu       sync.Mutex
	saved    []world.Checkpoint
	levelUps []world.LevelUpSave
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

func (f *fakeSaver) SaveCheckpoint(_ context.Context, cp world.Checkpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, cp)
	return f.attempt("cp " + cp.MapID)
}

func (f *fakeSaver) SaveLevelUp(_ context.Context, lu world.LevelUpSave) error {
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
		w := world.New(data, saver, slog.New(slog.DiscardHandler), world.Options{TickInterval: tickInterval, CheckpointInterval: 30 * time.Second})
		ctx, cancel := context.WithCancel(context.Background())
		runDone := make(chan error, 1)
		go func() { runDone <- w.Run(ctx) }()
		go w.RunPersistence(ctx)

		{ // one instance per map
			for _, m := range data.Maps() {
				inst, err := w.Instance(m.ID)
				if err != nil || inst.MapID() != m.ID {
					t.Fatalf("%s: %v", m.ID, err)
				}
			}
			if _, err := w.Instance("nowhere"); !errors.Is(err, world.ErrNoMap) {
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
			if snap := sa.all()[0].(world.Snapshot); snap.MapID != "town" || len(snap.Players) != 1 {
				t.Fatalf("town snapshot %+v", snap)
			}
			if snap := sb.all()[0].(world.Snapshot); snap.MapID != "forest" || len(snap.Players) != 1 {
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

// The persistence worker retries a failed save with backoff, in order, for
// up to a minute (page 10, decision 182): a character service restart must
// not lose a level-up, which no later checkpoint carries.
func TestThePersistenceWorkerRetriesInOrder(t *testing.T) {
	run := func(t *testing.T, saver *fakeSaver, jobs ...world.Job) {
		t.Helper()
		data := loadData(t)
		w := world.New(data, saver, slog.New(slog.DiscardHandler), world.Options{TickInterval: tickInterval, SaveRetryFor: time.Minute})
		for _, j := range jobs {
			if !w.Persist(j) {
				t.Fatal("queue refused a job")
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go w.RunPersistence(ctx)
		time.Sleep(2 * time.Minute)
		synctest.Wait()
	}
	levelUp := world.Job{LevelUp: &world.LevelUpSave{ID: kernel.NewCharacterID(), Level: 3}}
	checkpoint := world.Job{Checkpoint: &world.Checkpoint{ID: kernel.NewCharacterID(), MapID: "forest"}}
	t.Run("a save that fails twice is retried, and the next job waits for it", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			saver := &fakeSaver{err: errors.New("unavailable"), failures: 2}
			run(t, saver, levelUp, checkpoint)
			if got := saver.attemptsSoFar(); len(got) != 4 || got[0] != "lu 3" || got[2] != "lu 3" || got[3] != "cp forest" {
				t.Fatalf("attempts %v", got)
			}
		})
	})
	t.Run("one that keeps failing is given up after the limit, and the worker goes on", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			saver := &fakeSaver{err: errors.New("unavailable")}
			run(t, saver, levelUp, checkpoint)
			got := saver.attemptsSoFar()
			levelUps := 0
			for _, a := range got {
				if a == "lu 3" {
					levelUps++
				}
			}
			// Backoff from 100 ms doubling to 5 s fits about fifteen tries in a
			// minute; then the worker gives the level-up up and saves the
			// checkpoint, which keeps failing for its own minute.
			if levelUps < 3 || levelUps > 20 || got[levelUps] != "cp forest" {
				t.Fatalf("%d level-up attempts, then %v", levelUps, got[levelUps:])
			}
		})
	})
	t.Run("a refused save is not retried", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			saver := &fakeSaver{err: errRefused}
			run(t, saver, levelUp, checkpoint)
			if got := saver.attemptsSoFar(); len(got) != 2 {
				t.Fatalf("attempts %v", got)
			}
		})
	})
}

func TestShutdownFlushesWithoutRetrying(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		saver := &fakeSaver{err: errors.New("unavailable")}
		w := world.New(data, saver, slog.New(slog.DiscardHandler), world.Options{TickInterval: tickInterval, SaveRetryFor: time.Minute})
		w.Persist(world.Job{Checkpoint: &world.Checkpoint{ID: kernel.NewCharacterID(), MapID: "town"}})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		start := time.Now()
		if err := w.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if time.Since(start) > time.Second || len(saver.attemptsSoFar()) != 1 {
			t.Fatalf("flush took %s with %d attempts", time.Since(start), len(saver.attemptsSoFar()))
		}
	})
}

func TestSaverErrorsAreLoggedNotFatal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		saver := &fakeSaver{err: errors.New("db down")}
		w := world.New(data, saver, slog.New(slog.DiscardHandler), world.Options{TickInterval: tickInterval, CheckpointInterval: time.Second})
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
	inst := world.NewInstance(data.Town(), data, nil, world.Options{InboxSize: 1})
	// Not running: the first post fills the inbox, the second is refused.
	if !inst.Input(kernel.NewCharacterID(), 1, sim0()) {
		t.Fatal("first post refused")
	}
	if inst.Input(kernel.NewCharacterID(), 2, sim0()) {
		t.Fatal("second post accepted with a full inbox")
	}
	if err := inst.Join(context.Background(), newPlayer("Ayla", 0, 0), &sink{}); !errors.Is(err, world.ErrBusy) {
		t.Fatalf("join with a full inbox: %v", err)
	}
	if inst.Leave(kernel.NewCharacterID()) {
		t.Fatal("leave accepted with a full inbox")
	}
	t.Run("a join on a stopped instance gives up when the caller's context ends", func(t *testing.T) {
		inst := world.NewInstance(data.Town(), data, nil, world.Options{})
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		if err := inst.Join(ctx, newPlayer("Ayla", 0, 0), &sink{}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	})
}

func sim0() sim.Input { return sim.Input{} }
