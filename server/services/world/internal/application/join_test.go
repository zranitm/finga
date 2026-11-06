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

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/world/internal/application"
	"finga/services/world/internal/model"
)

var errNotYours = errors.New("not found")

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

func joinWorld(t *testing.T, chars *fakeCharacters, starter *fakeStarter, log ...*slog.Logger) (*application.World, func()) {
	t.Helper()
	data := loadData(t)
	o := options{Characters: chars, Starter: starter}
	if len(log) > 0 {
		o.Logger = log[0]
	}
	w := newWorldOf(data, &fakeSaver{}, o)
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
	t.Run("a failed grant is logged, and the join goes on", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			starter := &fakeStarter{err: errors.New("item service unavailable")}
			var logs bytes.Buffer
			w, stop := joinWorld(t, chars, starter, slog.New(slog.NewJSONHandler(&logs, nil)))
			defer stop()
			s := &sink{}
			if _, err := w.Join(context.Background(), account, ayla.ID, "conn-1", s); err != nil {
				t.Fatal(err)
			}
			time.Sleep(tickInterval)
			synctest.Wait()
			if snap, ok := s.all()[0].(model.Snapshot); !ok || snap.MapID != "town" {
				t.Fatalf("first event %+v", s.all())
			}
			if !strings.Contains(logs.String(), `"msg":"starter items not granted; the next join tries again"`) || !strings.Contains(logs.String(), `"character_id":"`+ayla.ID.String()+`"`) {
				t.Fatalf("log %q", logs.String())
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

// A stale connection's Leave, arriving after a rejoin, removes nothing.
func TestLeaveNamesItsConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		inst, stop := run(t, data, nil, options{})
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

// A character saved dead, before dead players were saved as respawned, joins as that respawn.
func TestACharacterSavedDeadJoinsRespawnedInTown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := loadData(t)
		forest, _ := data.Map("forest")
		account := kernel.NewAccountID()
		dead := model.Character{
			ID: kernel.NewCharacterID(), Account: account, Name: "Ayla", Class: kernel.Warrior, Level: 1,
			Stats: gamedata.Stats{STR: 12, DEX: 5, INT: 4, AGI: 5, VIT: 10}, HP: 0, MP: 28, MapID: forest.ID, X: 900, Y: 600,
		}
		w, stop := joinWorld(t, &fakeCharacters{chars: map[kernel.CharacterID]model.Character{dead.ID: dead}}, &fakeStarter{})
		defer stop()
		v, err := w.Join(context.Background(), account, dead.ID, "c-1", &sink{})
		if err != nil {
			t.Fatal(err)
		}
		town, _ := data.Map(forest.Town)
		if v.MapID != town.ID || v.X != town.PlayerSpawn.X || v.Y != town.PlayerSpawn.Y || v.HP != 102 {
			t.Fatalf("joined %+v, want %s's spawn with 102 HP", v, town.ID)
		}
	})
}
