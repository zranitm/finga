package world_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/character"
	"finga/internal/application/world"
	"finga/internal/application/world/sim"
	"finga/internal/model"
)

// fakeSaver records checkpoints, as the character service would.
type fakeSaver struct {
	mu       sync.Mutex
	saved    []character.Checkpoint
	levelUps []character.LevelUp
	err      error
}

func (f *fakeSaver) SaveCheckpoint(_ context.Context, cp character.Checkpoint) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved = append(f.saved, cp)
	return f.err
}

func (f *fakeSaver) SaveLevelUp(_ context.Context, lu character.LevelUp) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.levelUps = append(f.levelUps, lu)
	return f.err
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
	if !inst.Input(model.NewCharacterID(), 1, sim0()) {
		t.Fatal("first post refused")
	}
	if inst.Input(model.NewCharacterID(), 2, sim0()) {
		t.Fatal("second post accepted with a full inbox")
	}
	if err := inst.Join(context.Background(), newPlayer("Ayla", 0, 0), &sink{}); !errors.Is(err, world.ErrBusy) {
		t.Fatalf("join with a full inbox: %v", err)
	}
	if inst.Leave(model.NewCharacterID()) {
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
