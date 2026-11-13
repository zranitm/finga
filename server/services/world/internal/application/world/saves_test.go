package world_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/application/world"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

// db is the character service: down, a save fails; stuck, it waits out its deadline; loadErr fails every Load.
type db struct {
	mu      sync.Mutex
	chars   map[kernel.CharacterID]model.Character
	down    bool
	stuck   bool
	loadErr error
	failing int // the next saves to fail, as in a blip
}

func (d *db) Load(_ context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.loadErr != nil {
		return model.Character{}, d.loadErr
	}
	c, ok := d.chars[id]
	if !ok || c.Account != account {
		return model.Character{}, errNotYours
	}
	return c, nil
}

func (d *db) SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error {
	d.mu.Lock()
	if d.failing > 0 {
		d.failing--
		d.mu.Unlock()
		return errUnavailable
	}
	down, stuck := d.down, d.stuck
	if !down && !stuck {
		c := d.chars[cp.ID]
		c.MapID, c.X, c.Y, c.HP, c.MP, c.Level, c.Exp, c.Points = cp.MapID, cp.X, cp.Y, cp.HP, cp.MP, cp.Level, cp.Exp, cp.Points
		d.chars[cp.ID] = c
	}
	d.mu.Unlock()
	switch {
	case stuck:
		<-ctx.Done()
		return ctx.Err()
	case down:
		return errUnavailable
	}
	return nil
}

func (*db) Retryable(error) bool { return true }
func (*db) Refused(error) bool   { return false }

func (d *db) set(down, stuck bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.down, d.stuck = down, stuck
}

func (d *db) saved(id kernel.CharacterID) model.Character {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.chars[id]
}

// dbWorld runs a World over d, with no sweeps, until the test ends.
func dbWorld(t *testing.T, d *db) *worldtest.World {
	t.Helper()
	w := worldtest.NewWorld(worldtest.LoadData(t), d, worldtest.Options{Characters: d, CheckpointInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return w
}

// walkAway joins Ayla, saved at the town's spawn, through c-1 and walks her right, then stops her.
func walkAway(t *testing.T, w *worldtest.World, d *db) (model.Character, *worldtest.Sink, float64) {
	t.Helper()
	p := at(t, "Ayla", "town")
	ayla := model.Character{ID: p.ID, Account: kernel.NewAccountID(), Name: p.Name, Class: p.Class, Level: p.Level, Points: p.Points, HP: p.HP, MP: p.MP, MapID: "town", X: p.X, Y: p.Y}
	d.mu.Lock()
	d.chars[ayla.ID] = ayla
	d.mu.Unlock()
	s := &worldtest.Sink{}
	if _, err := w.Join(context.Background(), ayla.Account, ayla.ID, "c-1", s); err != nil {
		t.Fatal(err)
	}
	for seq := uint32(1); seq <= 6; seq++ {
		in := sim.Input{Right: seq < 6}
		if err := w.Input(ayla.ID, "c-1", seq, in, 0); err != nil {
			t.Fatal(err)
		}
		time.Sleep(worldtest.TickInterval)
	}
	time.Sleep(2 * worldtest.TickInterval)
	synctest.Wait()
	v, _ := s.Own(ayla.ID)
	if v.State.X <= ayla.X {
		t.Fatalf("Ayla did not move from %v", ayla.X)
	}
	return ayla, s, v.State.X
}

// A rejoin starts where the character is, not at its last save, whether that save landed, waits or is stuck.
func TestARejoinStartsFromTheNewestState(t *testing.T) {
	services := []struct {
		name        string
		down, stuck bool
	}{
		{name: "saved at once"},
		{name: "the character service down", down: true},
		{name: "the character service stuck", stuck: true},
	}
	departures := []struct {
		name   string
		conn   string // the new join's
		depart func(t *testing.T, w *world.World, id kernel.CharacterID, s *worldtest.Sink)
	}{
		{name: "a login elsewhere", conn: "c-2", depart: func(*testing.T, *world.World, kernel.CharacterID, *worldtest.Sink) {}},
		{name: "the gateway's stream broke", conn: "c-1", depart: func(_ *testing.T, _ *world.World, _ kernel.CharacterID, s *worldtest.Sink) {
			s.Refuse()
			time.Sleep(worldtest.TickInterval)
		}},
		{name: "a Leave, then a reconnect", conn: "c-2", depart: func(t *testing.T, w *world.World, id kernel.CharacterID, _ *worldtest.Sink) {
			if err := w.Leave(context.Background(), id, "c-1"); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, svc := range services {
		for _, dep := range departures {
			t.Run(svc.name+": "+dep.name, func(t *testing.T) {
				t.Parallel()
				synctest.Test(t, func(t *testing.T) {
					d := &db{chars: map[kernel.CharacterID]model.Character{}}
					w := dbWorld(t, d)
					ayla, old, x := walkAway(t, w, d)
					d.set(svc.down, svc.stuck)
					dep.depart(t, w.World, ayla.ID, old)

					s := &worldtest.Sink{}
					v, err := w.Join(context.Background(), ayla.Account, ayla.ID, dep.conn, s)
					if err != nil {
						t.Fatal(err)
					}
					if v.X != x || v.MapID != "town" {
						t.Fatalf("joined at %s %v, want town %v", v.MapID, v.X, x)
					}
					time.Sleep(worldtest.TickInterval)
					synctest.Wait()
					if snaps := worldtest.Of[model.Snapshot](s); len(snaps) != 1 || snaps[0].Players[len(snaps[0].Players)-1].State.X != x {
						t.Fatalf("snapshots %+v", snaps)
					}

					d.set(false, false)
					time.Sleep(time.Minute)
					synctest.Wait()
					if c := d.saved(ayla.ID); c.X != x {
						t.Fatalf("saved at %v once the character service was back, want %v", c.X, x)
					}
				})
			})
		}
	}
}

// Another account's join for a character fails, and takes it off no map.
func TestAJoinForSomeoneElsesCharacterLeavesItBe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &db{chars: map[kernel.CharacterID]model.Character{}}
		w := dbWorld(t, d)
		ayla, s, _ := walkAway(t, w, d)
		if _, err := w.Join(context.Background(), kernel.NewAccountID(), ayla.ID, "c-x", &worldtest.Sink{}); !errors.Is(err, errNotYours) {
			t.Fatalf("err = %v", err)
		}
		before := len(s.Deltas())
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		if len(s.Deltas()) == before {
			t.Fatal("Ayla left her map")
		}
	})
}

// Leave returns once the character is off its map, its last checkpoint queued.
func TestLeaveWaitsForTheMap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &db{chars: map[kernel.CharacterID]model.Character{}}
		w := dbWorld(t, d)
		ayla, _, x := walkAway(t, w, d)
		if err := w.Leave(context.Background(), ayla.ID, "c-1"); err != nil {
			t.Fatal(err)
		}
		synctest.Wait() // the save, with no tick between
		if c := d.saved(ayla.ID); c.X != x {
			t.Fatalf("saved at %v when Leave returned, want %v", c.X, x)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := w.Leave(ctx, ayla.ID, "c-1"); err != nil {
			t.Fatalf("a second Leave: %v", err)
		}
	})
}

// Two joins of one character at once each wait for the other's take-off, so neither starts from an older save.
func TestTwoJoinsAtOnceStartFromTheLiveState(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &db{chars: map[kernel.CharacterID]model.Character{}}
		w := dbWorld(t, d)
		ayla, _, x := walkAway(t, w, d)
		d.set(true, false)
		type joined struct {
			v   model.View
			err error
		}
		out := make(chan joined, 2)
		for _, conn := range []string{"c-2", "c-3"} {
			go func() {
				v, err := w.Join(context.Background(), ayla.Account, ayla.ID, conn, &worldtest.Sink{})
				out <- joined{v, err}
			}()
		}
		for range 2 {
			if j := <-out; j.err != nil || j.v.X != x {
				t.Fatalf("a join started at %v (err %v), want the live %v", j.v.X, j.err, x)
			}
		}
		d.set(false, false)
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := d.saved(ayla.ID).X; got != x {
			t.Fatalf("saved at %v, want %v", got, x)
		}
	})
}

// A login elsewhere that fails after the take-off detaches the older connection, so the gateway joins it again.
func TestAFailedLoginElsewhereDetachesTheOlderConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &db{chars: map[kernel.CharacterID]model.Character{}}
		w := dbWorld(t, d)
		ayla, old, x := walkAway(t, w, d)
		d.mu.Lock()
		d.loadErr = errUnavailable // the character service fails this one Load
		d.mu.Unlock()
		if _, err := w.Join(context.Background(), ayla.Account, ayla.ID, "c-2", &worldtest.Sink{}); !errors.Is(err, errUnavailable) {
			t.Fatalf("join: %v", err)
		}
		d.mu.Lock()
		d.loadErr = nil
		d.mu.Unlock()
		if n := detached(old); n != 1 {
			t.Fatalf("%d Detached events to the older connection, want one", n)
		}
		v, err := w.Join(context.Background(), ayla.Account, ayla.ID, "c-1", &worldtest.Sink{})
		if err != nil || v.X != x {
			t.Fatalf("the gateway's rejoin of c-1: %+v, %v; want x %v", v, err, x)
		}
	})
}

// Saves go on, whatever the process's context, until the maps have stopped, so the checkpoint each map puts as it stops is saved.
func TestSavesFlushOnlyAfterTheMapsStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &db{chars: map[kernel.CharacterID]model.Character{}}
		w := worldtest.NewWorld(worldtest.LoadData(t), d, worldtest.Options{Characters: d, CheckpointInterval: time.Hour})
		stoppingCtx, stop := context.WithCancel(context.Background())
		stop() // the process is already shutting down
		saved := make(chan error, 1)
		go func() { saved <- w.Save(stoppingCtx) }()
		ctx, cancel := context.WithCancel(context.Background())
		ran := make(chan error, 1)
		go func() { ran <- w.World.Run(ctx) }()
		ayla, _, x := walkAway(t, w, d)
		select {
		case err := <-saved:
			t.Fatalf("Save returned %v while the maps still ran", err)
		default:
		}
		cancel()
		if err := <-ran; err != nil {
			t.Fatal(err)
		}
		if err := <-saved; err != nil {
			t.Fatal(err)
		}
		if c := d.saved(ayla.ID); c.X != x {
			t.Fatalf("saved at %v, want the checkpoint the town put as it stopped, at %v", c.X, x)
		}
	})
}

// A store blip while the process winds down: the saves retry until the maps have stopped and the last checkpoint has landed.
func TestTheLastCheckpointLandsThroughABlipAtShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := &db{chars: map[kernel.CharacterID]model.Character{}}
		w := worldtest.NewWorld(worldtest.LoadData(t), d, worldtest.Options{Characters: d, CheckpointInterval: time.Hour})
		ctx, cancel := context.WithCancel(context.Background())
		ran := make(chan error, 1)
		go func() { ran <- w.Run(ctx) }()
		ayla, _, x := walkAway(t, w, d)
		d.mu.Lock()
		d.failing = 4
		d.mu.Unlock()
		cancel()
		if err := <-ran; err != nil {
			t.Fatal(err)
		}
		if c := d.saved(ayla.ID); c.X != x {
			t.Fatalf("saved at %v, want the checkpoint the town put as it stopped, at %v", c.X, x)
		}
	})
}
