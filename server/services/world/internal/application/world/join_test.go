package world_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

var errNotYours = errors.New("not found")

type fakeCharacters struct {
	chars map[kernel.CharacterID]model.Character
	slow  time.Duration // each Load takes this long

	mu            sync.Mutex
	loading, most int // Loads under way now, and at most at once
}

func (f *fakeCharacters) Load(_ context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	f.mu.Lock()
	f.loading++
	f.most = max(f.most, f.loading)
	f.mu.Unlock()
	time.Sleep(f.slow)
	f.mu.Lock()
	f.loading--
	f.mu.Unlock()
	c, ok := f.chars[id]
	if !ok || c.Account != account {
		return model.Character{}, errNotYours
	}
	c.Quests = c.Quests.Clone() // a fresh copy, as a load over the wire is
	return c, nil
}

type fakeStarter struct {
	mu     sync.Mutex
	grants []kernel.CharacterID
	err    error
}

func (f *fakeStarter) GrantStarter(_ context.Context, _ kernel.AccountID, id kernel.CharacterID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.grants = append(f.grants, id)
	return f.err
}

func joinWorld(t *testing.T, chars *fakeCharacters, starter *fakeStarter, log ...*slog.Logger) (*worldtest.World, func()) {
	t.Helper()
	data := worldtest.LoadData(t)
	o := worldtest.Options{Characters: chars, Starter: starter}
	if len(log) > 0 {
		o.Logger = log[0]
	}
	w := worldtest.NewWorld(data, &fakeSaver{}, o)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = w.Run(ctx) }()
	return w, func() { cancel(); <-done }
}

func TestJoinByID(t *testing.T) {
	account := kernel.NewAccountID()
	ayla := model.Character{
		ID: kernel.NewCharacterID(), Account: account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 1,
		HP: 999, Energy: 99, MapID: "town", X: 416, Y: 608,
	}
	lost := ayla
	lost.ID, lost.MapID = kernel.NewCharacterID(), "limbo"
	chars := &fakeCharacters{chars: map[kernel.CharacterID]model.Character{ayla.ID: ayla, lost.ID: lost}}

	t.Run("loads, grants the starter items, joins, and returns the view", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			starter := &fakeStarter{}
			w, stop := joinWorld(t, chars, starter)
			defer stop()
			s := &worldtest.Sink{}
			view, err := w.Join(context.Background(), account, ayla.ID, "conn-1", s)
			if err != nil {
				t.Fatal(err)
			}
			// HP and energy are clamped to their maximums, as the instance does.
			if view.ID != ayla.ID || view.Name != "Ayla" || view.MapID != "town" || view.MaxHP != 144 || view.HP != 144 || view.Energy != 10 || view.X != 416 {
				t.Fatalf("view %+v", view)
			}
			if len(starter.grants) != 1 || starter.grants[0] != ayla.ID {
				t.Fatalf("grants %v", starter.grants)
			}
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			if snap, ok := s.All()[0].(model.Snapshot); !ok || snap.MapID != "town" || len(snap.Players) != 1 {
				t.Fatalf("first event %+v", s.All())
			}
		})
	})
	t.Run("a failed grant is logged, and the join goes on", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			starter := &fakeStarter{err: errors.New("item service unavailable")}
			var logs bytes.Buffer
			w, stop := joinWorld(t, chars, starter, slog.New(slog.NewJSONHandler(&logs, nil)))
			defer stop()
			s := &worldtest.Sink{}
			if _, err := w.Join(context.Background(), account, ayla.ID, "conn-1", s); err != nil {
				t.Fatal(err)
			}
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			if snap, ok := s.All()[0].(model.Snapshot); !ok || snap.MapID != "town" {
				t.Fatalf("first event %+v", s.All())
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
			if _, err := w.Join(context.Background(), kernel.NewAccountID(), ayla.ID, "conn-1", &worldtest.Sink{}); !errors.Is(err, errNotYours) {
				t.Fatalf("err = %v", err)
			}
			if len(starter.grants) != 0 {
				t.Fatal("granted to a character that was not loaded")
			}
		})
	})
	t.Run("a later join of the character has a higher sequence", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			w, stop := joinWorld(t, chars, &fakeStarter{})
			defer stop()
			first, err := w.Join(context.Background(), account, ayla.ID, "conn-1", &worldtest.Sink{})
			if err != nil {
				t.Fatal(err)
			}
			second, err := w.Join(context.Background(), account, ayla.ID, "conn-2", &worldtest.Sink{})
			if err != nil {
				t.Fatal(err)
			}
			if first.JoinSeq == 0 || second.JoinSeq <= first.JoinSeq {
				t.Fatalf("sequences %d then %d; want a rising one", first.JoinSeq, second.JoinSeq)
			}
		})
	})
	t.Run("a character on a map the world does not run", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			w, stop := joinWorld(t, chars, &fakeStarter{})
			defer stop()
			if _, err := w.Join(context.Background(), account, lost.ID, "conn-1", &worldtest.Sink{}); !errors.Is(err, model.ErrNoMap) {
				t.Fatalf("err = %v", err)
			}
		})
	})
}

// A stale connection's Leave, arriving after a rejoin, removes nothing.
func TestLeaveNamesItsConnection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		inst, stop := worldtest.Run(t, data, nil, worldtest.Options{})
		defer stop()
		x, y := worldtest.TownSpawn(t, data)
		p := worldtest.NewPlayer("Ayla", x, y)
		p.Connection = "new"
		s := &worldtest.Sink{}
		if err := inst.Join(context.Background(), p, s); err != nil {
			t.Fatal(err)
		}
		_ = inst.Leave(p.ID, "old", nil)
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		before := len(s.Deltas())
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		if len(s.Deltas()) <= before {
			t.Fatal("the player was removed by another connection's Leave")
		}
		_ = inst.Leave(p.ID, "new", nil)
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		after := len(s.Deltas())
		time.Sleep(2 * worldtest.TickInterval)
		synctest.Wait()
		if len(s.Deltas()) != after {
			t.Fatal("the player is still on the map after its own Leave")
		}
	})
}

// A character saved dead, before dead players were saved as respawned, joins as that respawn.
func TestACharacterSavedDeadJoinsRespawnedInTown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		forest, _ := data.Map("forest")
		account := kernel.NewAccountID()
		dead := model.Character{
			ID: kernel.NewCharacterID(), Account: account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 1,
			HP: 0, MapID: forest.ID, X: 900, Y: 600,
		}
		w, stop := joinWorld(t, &fakeCharacters{chars: map[kernel.CharacterID]model.Character{dead.ID: dead}}, &fakeStarter{})
		defer stop()
		v, err := w.Join(context.Background(), account, dead.ID, "c-1", &worldtest.Sink{})
		if err != nil {
			t.Fatal(err)
		}
		town, _ := data.Map(forest.Town)
		if v.MapID != town.ID || v.X != town.PlayerSpawn.X || v.Y != town.PlayerSpawn.Y || v.HP != 72 {
			t.Fatalf("joined %+v, want %s's spawn with 72 HP", v, town.ID)
		}
	})
}

// A saved position the map can't hold, as a fall out of the world once left, starts at the map's spawn.
func TestASavedPositionOffTheMapStartsAtItsSpawn(t *testing.T) {
	data := worldtest.LoadData(t)
	forest, _ := data.Map("forest")
	for _, c := range []struct {
		name string
		x, y float64
		kept bool
	}{
		{"on the ground", 900, 608, true},
		{"fallen far below", 2306.86, 36703.93, false}, // a checkpoint saved while falling out of the world
		{"west of the map", -40, 608, false},
		{"inside a solid", 1000, 600, false},
		{"NaN", math.NaN(), 608, false},
		{"infinitely far down", 900, math.Inf(1), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				account := kernel.NewAccountID()
				saved := model.Character{
					ID: kernel.NewCharacterID(), Account: account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 1,
					HP: 100, MapID: forest.ID, X: c.x, Y: c.y,
				}
				w, stop := joinWorld(t, &fakeCharacters{chars: map[kernel.CharacterID]model.Character{saved.ID: saved}}, &fakeStarter{})
				defer stop()
				s := &worldtest.Sink{}
				v, err := w.Join(context.Background(), account, saved.ID, "c-1", s)
				if err != nil {
					t.Fatal(err)
				}
				want := forest.PlayerSpawn
				if c.kept {
					want = gamedata.Point{X: c.x, Y: c.y}
				}
				if v.MapID != forest.ID || v.X != want.X || v.Y != want.Y {
					t.Fatalf("joined %s at (%v, %v), want %s at (%v, %v)", v.MapID, v.X, v.Y, forest.ID, want.X, want.Y)
				}
				snap := s.All()[0].(model.Snapshot)
				if st := snap.Players[0].State; st.X != want.X || st.Y != want.Y {
					t.Fatalf("the map placed the player at (%v, %v), want (%v, %v)", st.X, st.Y, want.X, want.Y)
				}
			})
		})
	}
}

type hangingStarter struct{}

func (hangingStarter) GrantStarter(ctx context.Context, _ kernel.AccountID, _ kernel.CharacterID) error {
	<-ctx.Done() // an item service that takes the call and never answers
	return ctx.Err()
}

// The starter grant is best effort: an item service that never answers delays a join, never fails it.
func TestAHangingStarterGrantDoesNotFailTheJoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ayla := at(t, "Ayla", "town")
		chars := &fakeCharacters{chars: map[kernel.CharacterID]model.Character{ayla.ID: {ID: ayla.ID, Account: ayla.Account, Name: ayla.Name, Class: ayla.Class, Level: 1, Points: ayla.Points, HP: ayla.HP, Energy: ayla.Energy, MapID: "town", X: ayla.X, Y: ayla.Y}}}
		w := worldtest.NewWorld(worldtest.LoadData(t), &fakeSaver{}, worldtest.Options{Characters: chars, Starter: hangingStarter{}})
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = w.Run(ctx) }()
		defer func() { cancel(); <-done }()
		budgetCtx, stop := context.WithTimeout(context.Background(), 5*time.Second) // the gateway's join budget
		defer stop()
		start := time.Now()
		if _, err := w.Join(budgetCtx, ayla.Account, ayla.ID, "c-1", &worldtest.Sink{}); err != nil {
			t.Fatalf("join: %v", err)
		}
		if took := time.Since(start); took > 2*time.Second {
			t.Fatalf("the join took %s", took)
		}
	})
}

// A fresh join counts on the cooldowns and buffs the last checkpoint left, as a reload does: a running buff is in the joining sheet, and a timer that ended meanwhile is gone.
func TestAJoinCountsOnTheSkillTimersOfTheLastCheckpoint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		town := data.StartTown()
		account := kernel.NewAccountID()
		now := time.Now()
		ayla := model.Character{
			ID: kernel.NewCharacterID(), Account: account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 10,
			HP: 100, MapID: town.ID, X: town.PlayerSpawn.X, Y: town.PlayerSpawn.Y,
			Skills: model.Skills{Points: map[string]int{"iron_body": 1}},
			Timers: model.SkillTimers{
				Cooldowns: map[string]time.Time{"iron_body": now.Add(40 * time.Second), "focus": now.Add(-time.Second)},
				Buffs:     map[string]model.BuffTimer{"iron_body": {Until: now.Add(20 * time.Second), Points: 1}},
			},
		}
		w, stop := joinWorld(t, &fakeCharacters{chars: map[kernel.CharacterID]model.Character{ayla.ID: ayla}}, &fakeStarter{})
		defer stop()
		v, err := w.Join(context.Background(), account, ayla.ID, "c-1", &worldtest.Sink{})
		if err != nil {
			t.Fatal(err)
		}
		if _, ended := v.Timers.Cooldowns["focus"]; ended || !v.Timers.Cooldowns["iron_body"].Equal(now.Add(40*time.Second)) || v.Timers.Buffs["iron_body"].Points != 1 {
			t.Fatalf("joined with timers %+v, want Iron Body's cooldown and buff, Focus's ended", v.Timers)
		}
		if got := v.Sheet.DerivedSkills.PhysicalDefense; got != 22 {
			t.Fatalf("the joining sheet's skills' physical defense is %v, want Iron Body's 22 at 1 point", got)
		}
	})
}
