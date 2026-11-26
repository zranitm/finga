package world_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/application/world/worldtest"
	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
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
	c.Quests = c.Quests.Clone() // a fresh copy, as the character module's facade gives
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
		HP: 999, Energy: 99, MapID: "crossroads", X: 20, Z: 10,
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
			if view.ID != ayla.ID || view.Name != "Ayla" || view.MapID != "crossroads" || view.MaxHP != 144 || view.HP != 144 || view.Energy != 10 {
				t.Fatalf("view %+v", view)
			}
			if len(starter.grants) != 1 || starter.grants[0] != ayla.ID {
				t.Fatalf("grants %v", starter.grants)
			}
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			if snap, ok := s.All()[0].(model.Snapshot); !ok || snap.MapID != "crossroads" || len(snap.Players) != 1 {
				t.Fatalf("first event %+v", s.All())
			}
			if at := joinedAt(t, s, ayla.ID); at.X != ayla.X || at.Y != 0 || at.Z != ayla.Z {
				t.Fatalf("joined at %+v, want her save's (%v, %v) on the ground", at, ayla.X, ayla.Z)
			}
		})
	})
	t.Run("a failed grant is logged, and the join goes on", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			starter := &fakeStarter{err: errors.New("item module unavailable")}
			var logs bytes.Buffer
			w, stop := joinWorld(t, chars, starter, slog.New(slog.NewJSONHandler(&logs, nil)))
			defer stop()
			s := &worldtest.Sink{}
			if _, err := w.Join(context.Background(), account, ayla.ID, "conn-1", s); err != nil {
				t.Fatal(err)
			}
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			if snap, ok := s.All()[0].(model.Snapshot); !ok || snap.MapID != "crossroads" {
				t.Fatalf("first event %+v", s.All())
			}
			if !strings.Contains(logs.String(), `"msg":"starter items not granted; the next join tries again"`) || !strings.Contains(logs.String(), `"character_id":"`+ayla.ID.String()+`"`) {
				t.Fatalf("log %q", logs.String())
			}
		})
	})
	t.Run("someone else's character fails as the character module said", func(t *testing.T) {
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
	t.Run("a character saved on a map that's gone starts at the start map", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var logs bytes.Buffer
			w, stop := joinWorld(t, chars, &fakeStarter{}, slog.New(slog.NewJSONHandler(&logs, nil)))
			defer stop()
			s := &worldtest.Sink{}
			view, err := w.Join(context.Background(), account, lost.ID, "conn-1", s)
			if err != nil {
				t.Fatal(err)
			}
			start := worldtest.LoadData(t).StartTown()
			time.Sleep(worldtest.TickInterval)
			synctest.Wait()
			if strings.Count(logs.String(), `"level":"WARN"`) != 1 || strings.Contains(logs.String(), "can't hold") {
				t.Fatalf("log %q; want the one warning, for the map that's gone", logs.String())
			}
			if at := joinedAt(t, s, lost.ID); view.MapID != start.ID || math.Abs(at.X-start.PlayerSpawn.X) > 3 || math.Abs(at.Z-start.PlayerSpawn.Z) > 3 {
				t.Fatalf("joined %s at %+v; want %s by its spawn %v", view.MapID, at, start.ID, start.PlayerSpawn)
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
		forest, _ := data.Map("dune_road")
		account := kernel.NewAccountID()
		dead := model.Character{
			ID: kernel.NewCharacterID(), Account: account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 1,
			HP: 0, MapID: forest.ID, X: 30, Z: 20,
		}
		w, stop := joinWorld(t, &fakeCharacters{chars: map[kernel.CharacterID]model.Character{dead.ID: dead}}, &fakeStarter{})
		defer stop()
		s := &worldtest.Sink{}
		v, err := w.Join(context.Background(), account, dead.ID, "c-1", s)
		if err != nil {
			t.Fatal(err)
		}
		town, _ := data.Map(forest.Town)
		if at := joinedAt(t, s, dead.ID); v.MapID != town.ID || at.X != town.PlayerSpawn.X || at.Z != town.PlayerSpawn.Z || at.Y != 0 || v.HP != 72 {
			t.Fatalf("joined %+v at %+v, want %s's spawn with 72 HP", v, at, town.ID)
		}
	})
}

// From the waypoints playtest: one saved dead in a region joins where they'd have got up, beside its nearest waypoint, not at its start.
func TestACharacterSavedDeadInARegionJoinsAtItsNearestWaypoint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		region, woods, ok := data.Waypoint("woods")
		if !ok || region.ID != "test_region" {
			t.Fatal("no East Woods waypoint in the test region")
		}
		account := kernel.NewAccountID()
		dead := model.Character{
			ID: kernel.NewCharacterID(), Account: account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 1,
			HP: 0, MapID: region.ID, X: 212.5, Z: 100, // by the East Woods' glow slimes
		}
		w, stop := joinWorld(t, &fakeCharacters{chars: map[kernel.CharacterID]model.Character{dead.ID: dead}}, &fakeStarter{})
		defer stop()
		s := &worldtest.Sink{}
		v, err := w.Join(context.Background(), account, dead.ID, "c-1", s)
		if err != nil {
			t.Fatal(err)
		}
		if at := joinedAt(t, s, dead.ID); v.MapID != region.ID || at.X != woods.Arrive.X || at.Z != woods.Arrive.Z || v.HP != 72 {
			t.Fatalf("joined %+v at %+v; want beside East Woods' stone at %v with 72 HP", v, at, woods.Arrive)
		}
	})
}

// A saved position the map can't hold starts at the map's spawn; one under an open spot stands on the ground above it.
func TestASavedPositionOffTheMapStartsAtItsSpawn(t *testing.T) {
	data := worldtest.LoadData(t)
	forest, _ := data.Map("dune_road")
	const hillX, hillZ = 24.0, 14.0 // the top of a hill, clear of solids
	hill := forest.Geometry.Terrain.Height(hillX, hillZ)
	for _, c := range []struct {
		name    string
		x, y, z float64
		kept    bool
	}{
		{"on the ground", hillX, hill, hillZ, true},
		{"fallen far below an open spot stands on the ground there", hillX, -36703.93, hillZ, true}, // a checkpoint saved while falling
		{"infinitely far below an open spot stands on the ground there", hillX, math.Inf(-1), hillZ, true},
		{"west of the map", -40, 0, 20, false},
		{"inside a solid", 29, 0, 17.5, false}, // a pillar on the road
		{"NaN", math.NaN(), 0, 20, false},
		{"infinitely far east", math.Inf(1), 0, 20, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				account := kernel.NewAccountID()
				saved := model.Character{
					ID: kernel.NewCharacterID(), Account: account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 1,
					HP: 100, MapID: forest.ID, X: c.x, Y: c.y, Z: c.z,
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
					want = gamedata.Point{X: c.x, Z: c.z}
				}
				ground := forest.Geometry.Terrain.Height(want.X, want.Z)
				if st := joinedAt(t, s, saved.ID); v.MapID != forest.ID || st.X != want.X || st.Y != ground || st.Z != want.Z || !st.OnGround {
					t.Fatalf("joined %s at %+v, want %s at (%v, %v, %v) on the ground", v.MapID, st, forest.ID, want.X, ground, want.Z)
				}
			})
		})
	}
}

type hangingStarter struct{}

func (hangingStarter) GrantStarter(ctx context.Context, _ kernel.AccountID, _ kernel.CharacterID) error {
	<-ctx.Done() // an item module that takes the call and never answers
	return ctx.Err()
}

// The starter grant is best effort: an item module that never answers delays a join, never fails it.
func TestAHangingStarterGrantDoesNotFailTheJoin(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ayla := at(t, "Ayla", "crossroads")
		chars := &fakeCharacters{chars: map[kernel.CharacterID]model.Character{ayla.ID: {ID: ayla.ID, Account: ayla.Account, Name: ayla.Name, Class: ayla.Class, Level: 1, Points: ayla.Points, HP: ayla.HP, Energy: ayla.Energy, MapID: "crossroads", X: ayla.X, Y: ayla.Y, Z: ayla.Z}}}
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

// A fresh join counts on the cooldowns and buffs the last checkpoint left, as a reload does: a running buff is in the joining sheet with the trait it was used with, and a timer that ended meanwhile is gone. The sheet lists the buffs (C22): the player's own with no giver, a party member's with theirs, as a pending checkpoint keeps it.
func TestAJoinCountsOnTheSkillTimersOfTheLastCheckpoint(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		data := worldtest.LoadData(t)
		town := data.StartTown()
		account := kernel.NewAccountID()
		now := time.Now()
		ayla, bram := model.Character{
			ID: kernel.NewCharacterID(), Account: account, Name: "Ayla", Class: kernel.Class("warrior"), Level: 10,
			HP: 100, MapID: town.ID, X: town.PlayerSpawn.X, Z: town.PlayerSpawn.Z,
			Skills: model.Skills{Points: map[string]int{"iron_body": 1, "war_cry": 10}, Traits: map[string]string{"war_cry": "fury"}},
			Timers: model.SkillTimers{
				Cooldowns: map[string]time.Time{"iron_body": now.Add(40 * time.Second), "focus": now.Add(-time.Second)},
				Buffs: map[string]model.BuffTimer{
					"iron_body": {Until: now.Add(20 * time.Second), Points: 1},
					"war_cry":   {Until: now.Add(20 * time.Second), Points: 10, Trait: "fury"},
					"rally":     {Until: now.Add(-time.Second), Points: 1},
				},
			},
		}, kernel.NewCharacterID()
		ayla.Timers.Buffs["iron_body"] = model.BuffTimer{Until: now.Add(20 * time.Second), Points: 1, From: ayla.ID}
		ayla.Timers.Buffs["war_cry"] = model.BuffTimer{Until: now.Add(20 * time.Second), Points: 10, Trait: "fury", From: bram}
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
		if got := v.Sheet.DerivedSkills.PhysicalAttack; got != 36 || v.Timers.Buffs["war_cry"].Trait != "fury" || v.Sheet.Skills.Traits["war_cry"] != "fury" {
			t.Fatalf("the joining sheet's skills' physical attack is %v, timers %+v, traits %+v; want Fury's 16 + 2 a point at 10, its buff and choice kept", got, v.Timers.Buffs, v.Sheet.Skills.Traits)
		}
		want := []model.BuffView{{Skill: "iron_body", Left: 20 * time.Second, Points: 1}, {Skill: "war_cry", Left: 20 * time.Second, Points: 10, From: bram}}
		if !slices.Equal(v.Sheet.Buffs, want) {
			t.Fatalf("the joining sheet lists buffs %+v, want %+v: Iron Body her own, War Cry from Bram, Rally ended", v.Sheet.Buffs, want)
		}
	})
}
