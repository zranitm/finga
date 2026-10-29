package application_test

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/world/internal/application"
	"finga/services/world/internal/model"
)

var errNotYours = errors.New("not found")

// fakeCharacters is the character service's Load.
type fakeCharacters struct {
	chars map[kernel.CharacterID]model.Character
}

func (f *fakeCharacters) Load(_ context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	c, ok := f.chars[id]
	if !ok || c.Account != account {
		return model.Character{}, errNotYours
	}
	return c, nil
}

// fakeStarter records grants, or fails with err.
type fakeStarter struct {
	mu     sync.Mutex
	grants []kernel.CharacterID
	err    error
}

func (f *fakeStarter) GrantStarter(_ context.Context, _ kernel.AccountID, id kernel.CharacterID, _ kernel.Class) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants = append(f.grants, id)
	return f.err
}

func joinWorld(t *testing.T, chars *fakeCharacters, starter *fakeStarter) (*application.World, func()) {
	t.Helper()
	data := loadData(t)
	w := application.New(data, &fakeSaver{}, slog.New(slog.DiscardHandler), application.Options{TickInterval: tickInterval, Characters: chars, Starter: starter})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	return w, func() { cancel(); <-done }
}

func TestJoinByID(t *testing.T) {
	account := kernel.NewAccountID()
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: account, Name: "Ayla", Class: kernel.Warrior, Level: 1,
		Stats: gamedata.Stats{STR: 12, DEX: 5, INT: 4, AGI: 5, VIT: 10}, HP: 999, MP: 28, MapID: "town", X: 416, Y: 608,
	}
	lost := ayla
	lost.ID, lost.MapID = kernel.NewCharacterID(), "limbo"
	chars := &fakeCharacters{chars: map[kernel.CharacterID]model.Character{ayla.ID: ayla, lost.ID: lost}}

	t.Run("loads, grants the starter items, joins, and returns the view", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			starter := &fakeStarter{}
			w, stop := joinWorld(t, chars, starter)
			defer stop()
			s := &sink{}
			view, err := w.Join(context.Background(), account, ayla.ID, "conn-1", s)
			if err != nil {
				t.Fatal(err)
			}
			// HP is clamped to the derived maximum, as the instance does.
			if view.ID != ayla.ID || view.Name != "Ayla" || view.MapID != "town" || view.MaxHP != 204 || view.HP != 204 || view.MaxMP != 28 || view.X != 416 {
				t.Fatalf("view %+v", view)
			}
			if len(starter.grants) != 1 || starter.grants[0] != ayla.ID {
				t.Fatalf("grants %v", starter.grants)
			}
			time.Sleep(tickInterval)
			synctest.Wait()
			if snap, ok := s.all()[0].(model.Snapshot); !ok || snap.MapID != "town" || len(snap.Players) != 1 {
				t.Fatalf("first event %+v", s.all())
			}
		})
	})
	t.Run("a failed grant fails the join, and nobody joins", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			starter := &fakeStarter{err: errors.New("item service unavailable")}
			w, stop := joinWorld(t, chars, starter)
			defer stop()
			s := &sink{}
			if _, err := w.Join(context.Background(), account, ayla.ID, "conn-1", s); err == nil {
				t.Fatal("joined although the grant failed")
			}
			time.Sleep(2 * tickInterval)
			synctest.Wait()
			if len(s.all()) != 0 {
				t.Fatalf("events %+v", s.all())
			}
		})
	})
	t.Run("someone else's character fails as the character service said", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			starter := &fakeStarter{}
			w, stop := joinWorld(t, chars, starter)
			defer stop()
			if _, err := w.Join(context.Background(), kernel.NewAccountID(), ayla.ID, "conn-1", &sink{}); !errors.Is(err, errNotYours) {
				t.Fatalf("err = %v", err)
			}
			if len(starter.grants) != 0 {
				t.Fatal("granted to a character that was not loaded")
			}
		})
	})
	t.Run("a character on a map the world does not run", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			w, stop := joinWorld(t, chars, &fakeStarter{})
			defer stop()
			if _, err := w.Join(context.Background(), account, lost.ID, "conn-1", &sink{}); !errors.Is(err, model.ErrNoMap) {
				t.Fatalf("err = %v", err)
			}
		})
	})
}

// A Leave names the connection that joined: an older connection's Leave,
// arriving after a newer connection rejoined the character, removes nothing.
func TestLeaveNamesItsConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		inst, stop := run(t, data, nil, application.Options{})
		defer stop()
		x, y := townSpawn(t, data)
		p := newPlayer("Ayla", x, y)
		p.Connection = "new"
		s := &sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		inst.LeaveConnection(p.ID, "old")
		time.Sleep(2 * tickInterval)
		synctest.Wait()
		before := len(s.deltas())
		time.Sleep(2 * tickInterval)
		synctest.Wait()
		if len(s.deltas()) <= before {
			t.Fatal("the player was removed by another connection's Leave")
		}
		inst.LeaveConnection(p.ID, "new")
		time.Sleep(2 * tickInterval)
		synctest.Wait()
		after := len(s.deltas())
		time.Sleep(2 * tickInterval)
		synctest.Wait()
		if len(s.deltas()) != after {
			t.Fatal("the player is still on the map after its own Leave")
		}
	})
}
