package application_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/services/world/internal/application"
	"finga/services/world/internal/model"
)

// db is the character service: Load returns what was saved. Down, a save fails; stuck, it waits out its
// deadline.
type db struct {
	mu    sync.Mutex
	chars map[kernel.CharacterID]model.Character
	down  bool
	stuck bool
}

func (d *db) Load(_ context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, ok := d.chars[id]
	if !ok || c.Account != account {
		return model.Character{}, errNotYours
	}
	return c, nil
}

func (d *db) SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error {
	d.mu.Lock()
	down, stuck := d.down, d.stuck
	if !down && !stuck {
		c := d.chars[cp.ID]
		c.MapID, c.X, c.Y, c.HP, c.MP, c.Level, c.Exp, c.Stats = cp.MapID, cp.X, cp.Y, cp.HP, cp.MP, cp.Level, cp.Exp, cp.Stats
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
func dbWorld(t *testing.T, d *db) *application.World {
	t.Helper()
	w := newWorldOf(loadData(t), d, options{Characters: d, CheckpointInterval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	return w
}

// walkAway joins Ayla, saved at the town's spawn, through c-1 and walks her right, then stops her. It
// returns her, the sink she joined with and where she stands.
func walkAway(t *testing.T, w *application.World, d *db) (model.Character, *sink, float64) {
	t.Helper()
	p := at(t, "Ayla", "town")
	ayla := model.Character{ID: p.ID, Account: kernel.NewAccountID(), Name: p.Name, Class: p.Class, Level: p.Level, Stats: p.Stats, HP: p.HP, MP: p.MP, MapID: "town", X: p.X, Y: p.Y}
	d.mu.Lock()
	d.chars[ayla.ID] = ayla
	d.mu.Unlock()
	s := &sink{}
	if _, err := w.Join(context.Background(), ayla.Account, ayla.ID, "c-1", s); err != nil {
		t.Fatal(err)
	}
	for seq := uint32(1); seq <= 6; seq++ {
		in := sim.Input{Right: seq < 6}
		if err := w.Input(ayla.ID, "c-1", seq, in); err != nil {
			t.Fatal(err)
		}
		time.Sleep(tickInterval)
	}
	time.Sleep(2 * tickInterval)
	synctest.Wait()
	v, _ := s.own(ayla.ID)
	if v.State.X <= ayla.X {
		t.Fatalf("Ayla did not move from %v", ayla.X)
	}
	return ayla, s, v.State.X
}

// A join for a character still in the world starts from where the character is, not from its last save,
// whether that save has landed, waits on a character service that is down, or is stuck in one.
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
		depart func(t *testing.T, w *application.World, id kernel.CharacterID, s *sink)
	}{
		{name: "a login elsewhere", conn: "c-2", depart: func(*testing.T, *application.World, kernel.CharacterID, *sink) {}},
		{name: "the gateway's stream broke", conn: "c-1", depart: func(_ *testing.T, _ *application.World, _ kernel.CharacterID, s *sink) {
			s.mu.Lock()
			s.full = true
			s.mu.Unlock()
			time.Sleep(tickInterval)
		}},
		{name: "a Leave, then a reconnect", conn: "c-2", depart: func(t *testing.T, w *application.World, id kernel.CharacterID, _ *sink) {
			if err := w.Leave(context.Background(), id, "c-1"); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, svc := range services {
		for _, dep := range departures {
			t.Run(svc.name+": "+dep.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					d := &db{chars: map[kernel.CharacterID]model.Character{}}
					w := dbWorld(t, d)
					ayla, old, x := walkAway(t, w, d)
					d.set(svc.down, svc.stuck)
					dep.depart(t, w, ayla.ID, old)

					s := &sink{}
					v, err := w.Join(context.Background(), ayla.Account, ayla.ID, dep.conn, s)
					if err != nil {
						t.Fatal(err)
					}
					if v.X != x || v.MapID != "town" {
						t.Fatalf("joined at %s %v, want town %v", v.MapID, v.X, x)
					}
					time.Sleep(tickInterval)
					synctest.Wait()
					if snaps := snapshots(s); len(snaps) != 1 || snaps[0].Players[len(snaps[0].Players)-1].State.X != x {
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
		if _, err := w.Join(context.Background(), kernel.NewAccountID(), ayla.ID, "c-x", &sink{}); !errors.Is(err, errNotYours) {
			t.Fatalf("err = %v", err)
		}
		before := len(s.deltas())
		time.Sleep(2 * tickInterval)
		synctest.Wait()
		if len(s.deltas()) == before {
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
