package application_test

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"slices"
	"testing"
	"testing/synctest"
	"time"

	"finga/internal/gamedata"
	"finga/internal/gamedata/gamedatatest"
	"finga/internal/kernel"
	"finga/services/character/internal/application"
	"finga/services/character/internal/model"
)

func newService(t *testing.T) (*application.Service, *fakeStore, *gamedata.Data) {
	t.Helper()
	return newServiceWith(t, application.Deps{})
}

// newServiceWith builds the service over a fake store and the real game data, with deps' starter and logger when set.
func newServiceWith(t *testing.T, deps application.Deps) (*application.Service, *fakeStore, *gamedata.Data) {
	t.Helper()
	data := gamedatatest.Shipped(t)
	store := newFakeStore()
	deps.Store, deps.Content = store, data
	if deps.Starter == nil {
		deps.Starter = &fakeStarter{}
	}
	if deps.Logger == nil {
		deps.Logger = slog.New(slog.DiscardHandler)
	}
	return application.NewService(deps), store, data
}

func TestCreate(t *testing.T) {
	ctx := context.Background()
	owner := kernel.NewAccountID()

	t.Run("valid", func(t *testing.T) {
		svc, store, data := newService(t)
		c, err := svc.Create(ctx, owner, application.CreateInput{Name: "Ayla"})
		if err != nil {
			t.Fatal(err)
		}
		town := data.StartTown()
		checks := []struct {
			name string
			ok   bool
		}{
			{"owned by the account", c.AccountID == owner},
			{"name kept as given", c.Name == "Ayla"},
			{"a Beginner, the root of the class tree", c.Class == "beginner" && c.Class == data.StartClass().ID},
			{"level 1 with no EXP", c.Level == 1 && c.Exp == 0},
			{"no points, so its own stats are the starting stats", c.Points == (gamedata.Stats{}) && own(svc, c) == data.StartClass().BaseStats},
			{"no unspent points, and the free reset left", c.UnspentPoints == 0 && !c.FreeResetUsed},
			{"HP and MP full: 135 and 48 for a level 1 Beginner", c.HP == 135 && c.MP == 48},
			{"starts in the town at its player spawn", c.MapID == town.ID && c.X == town.PlayerSpawn.X && c.Y == town.PlayerSpawn.Y},
			{"stored", len(store.chars) == 1},
			{"returned as stored, with the store's updated_at", reflect.DeepEqual(c, store.chars[c.ID]) && !c.UpdatedAt.IsZero()},
		}
		for _, ck := range checks {
			if !ck.ok {
				t.Fatalf("%s: %+v", ck.name, c)
			}
		}
	})

	t.Run("invalid names", func(t *testing.T) {
		tests := []struct {
			name  string
			input string
		}{
			{"too short", "Ay"},
			{"too long", "Aylaaylaaylaa"},
			{"starts with a digit", "1Ayla"},
			{"contains a space", "Ay la"},
			{"contains an underscore", "Ay_la"},
			{"contains unicode", "Aylä"},
			{"empty", ""},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				svc, store, _ := newService(t)
				_, err := svc.Create(ctx, owner, application.CreateInput{Name: tt.input})
				if !errors.Is(err, model.ErrBadName) {
					t.Fatalf("err = %v", err)
				}
				if store.txCount != 0 {
					t.Fatal("an invalid name reached the store")
				}
			})
		}
	})

	t.Run("boundary names are accepted", func(t *testing.T) {
		svc, _, _ := newService(t)
		for _, name := range []string{"Ayl", "Aylaaylaayla", "A1b2"} {
			if _, err := svc.Create(ctx, owner, application.CreateInput{Name: name}); err != nil {
				t.Fatalf("%q: %v", name, err)
			}
		}
	})

	t.Run("a duplicate name becomes conflict", func(t *testing.T) {
		svc, _, _ := newService(t)
		if _, err := svc.Create(ctx, owner, application.CreateInput{Name: "Ayla"}); err != nil {
			t.Fatal(err)
		}
		_, err := svc.Create(ctx, kernel.NewAccountID(), application.CreateInput{Name: "AYLA"})
		if !errors.Is(err, model.ErrNameTaken) {
			t.Fatalf("err = %v", err)
		}
	})
}

// The skill bar holds six keys, each empty or an active skill in the game data; only the owner sets it.
func TestSetSkillBar(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newService(t)
	alice, bob := kernel.NewAccountID(), kernel.NewAccountID()
	a, _ := svc.Create(ctx, alice, application.CreateInput{Name: "Ayla"})
	for _, c := range []struct {
		name string
		bar  []string
	}{
		{"five keys", []string{"strike", "", "", "", ""}},
		{"seven keys", []string{"strike", "", "", "", "", "", ""}},
		{"a skill the data doesn't have", []string{"fireball", "", "", "", "", ""}},
		{"a passive skill", []string{"second_wind", "", "", "", "", ""}},
	} {
		if err := svc.SetSkillBar(ctx, a.ID, alice, c.bar); !errors.Is(err, model.ErrBadSkillBar) {
			t.Fatalf("%s: %v, want ErrBadSkillBar", c.name, err)
		}
	}
	bar := []string{"strike", "", "spark", "", "", "throw"}
	if err := svc.SetSkillBar(ctx, a.ID, bob, bar); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("someone else's: %v, want ErrNotFound", err)
	}
	if err := svc.SetSkillBar(ctx, a.ID, alice, bar); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Load(ctx, a.ID, alice); !slices.Equal(got.SkillBar, bar) {
		t.Fatalf("skill bar %q, want %q", got.SkillBar, bar)
	}
}

func TestListAndLoad(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newService(t)
	alice, bob := kernel.NewAccountID(), kernel.NewAccountID()
	a1, _ := svc.Create(ctx, alice, application.CreateInput{Name: "Ayla"})
	a2, _ := svc.Create(ctx, alice, application.CreateInput{Name: "Robin"})
	if _, err := svc.Create(ctx, bob, application.CreateInput{Name: "Bruno"}); err != nil {
		t.Fatal(err)
	}

	t.Run("list shows only the account's characters", func(t *testing.T) {
		list, err := svc.List(ctx, alice)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].ID != a1.ID || list[1].ID != a2.ID {
			t.Fatalf("list = %+v", list)
		}
		empty, err := svc.List(ctx, kernel.NewAccountID())
		if err != nil || len(empty) != 0 {
			t.Fatalf("unknown account: %v, %v", empty, err)
		}
	})

	t.Run("load one of your own", func(t *testing.T) {
		got, err := svc.Load(ctx, a1.ID, alice)
		if err != nil || got.Name != "Ayla" {
			t.Fatalf("got %+v, %v", got, err)
		}
	})

	t.Run("someone else's character gives not_found", func(t *testing.T) {
		_, err := svc.Load(ctx, a1.ID, bob)
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("an unknown ID gives not_found", func(t *testing.T) {
		_, err := svc.Load(ctx, kernel.NewCharacterID(), alice)
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestSaves(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newService(t)
	owner := kernel.NewAccountID()
	c, err := svc.Create(ctx, owner, application.CreateInput{Name: "Ayla"})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a checkpoint updates the class, position, HP, MP and the whole progression", func(t *testing.T) {
		cp := model.Checkpoint{ID: c.ID, Class: "warrior", MapID: "forest", X: 300, Y: 608, HP: 150, MP: 20, Level: 2, Exp: 12, Points: gamedata.Stats{STR: 3}, UnspentPoints: 2, FreeResetUsed: true}
		if err := svc.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatal(err)
		}
		got := store.chars[c.ID]
		if got.Class != "warrior" || got.MapID != "forest" || got.X != 300 || got.Y != 608 || got.HP != 150 || got.MP != 20 || got.Exp != 12 || got.Level != 2 ||
			got.Points != cp.Points || got.UnspentPoints != 2 || !got.FreeResetUsed {
			t.Fatalf("after checkpoint: %+v", got)
		}
	})

	t.Run("a level-up updates level, EXP, points, HP and MP", func(t *testing.T) {
		lu := model.LevelUp{ID: c.ID, Level: 2, Exp: 3, Points: gamedata.Stats{STR: 3, VIT: 2}, HP: 250, MP: 32}
		if err := svc.SaveLevelUp(ctx, lu); err != nil {
			t.Fatal(err)
		}
		got := store.chars[c.ID]
		if got.Level != 2 || got.Exp != 3 || got.Points != lu.Points || got.HP != 250 || got.MP != 32 {
			t.Fatalf("after level-up: %+v", got)
		}
	})

	t.Run("a checkpoint saves the whole quest log", func(t *testing.T) {
		cp := model.Checkpoint{ID: c.ID, Class: "beginner", MapID: "town", Level: 2, Quests: []model.Quest{
			{ID: "slime_cleanup", Status: model.QuestActive, Progress: []int{4}},
			{ID: "sticky_samples", Status: model.QuestFinished, TimesFinished: 1},
		}}
		if err := svc.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatal(err)
		}
		cp.Quests = []model.Quest{{ID: "sticky_samples", Status: model.QuestActive, Progress: []int{0}, TimesFinished: 1}}
		if err := svc.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatal(err)
		}
		got, err := svc.Quests(ctx, c.ID)
		if err != nil || !reflect.DeepEqual(got, cp.Quests) {
			t.Fatalf("quests %+v, %v", got, err)
		}
	})

	t.Run("a checkpoint with a quest the table can't hold saves nothing", func(t *testing.T) {
		for _, bad := range []model.Checkpoint{
			{ID: c.ID, Class: "beginner", Quests: []model.Quest{{Status: model.QuestActive}}},
			{ID: c.ID, Class: "beginner", Quests: []model.Quest{{ID: "q", Status: "paused"}}},
			{ID: c.ID, Class: "beginner", Quests: []model.Quest{{ID: "q", Status: model.QuestActive, Progress: []int{-1}}}},
			{ID: c.ID, Class: "beginner", Quests: []model.Quest{{ID: "q", Status: model.QuestFinished}}},
			{ID: c.ID, Class: "beginner", Quests: []model.Quest{{ID: "q", Status: model.QuestFinished, Progress: []int{3}, TimesFinished: 1}}},
			{ID: c.ID, Class: "beginner", Quests: []model.Quest{{ID: "q", Status: model.QuestActive, TimesFinished: -1}}},
			{ID: c.ID, Class: "beginner", Quests: []model.Quest{{ID: "q", Status: model.QuestActive}, {ID: "q", Status: model.QuestActive}}},
		} {
			if err := svc.SaveCheckpoint(ctx, bad); !errors.Is(err, model.ErrBadQuest) {
				t.Fatalf("%+v: %v", bad, err)
			}
		}
		if store.chars[c.ID].MapID != "town" {
			t.Fatal("a refused checkpoint moved the character")
		}
	})

	t.Run("a checkpoint saves the skill points", func(t *testing.T) {
		cp := model.Checkpoint{ID: c.ID, Class: "beginner", MapID: "town", Level: 2, UnspentSkillPoints: 2, Skills: []model.SkillPoints{{ID: "strike", Points: 3}}}
		if err := svc.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatal(err)
		}
		got, err := svc.Skills(ctx, c.ID)
		if err != nil || !reflect.DeepEqual(got, cp.Skills) || store.chars[c.ID].UnspentSkillPoints != 2 {
			t.Fatalf("skills %+v (%v), unspent %d", got, err, store.chars[c.ID].UnspentSkillPoints)
		}
	})

	t.Run("a checkpoint with a skill the table can't hold saves nothing", func(t *testing.T) {
		for _, bad := range []model.Checkpoint{
			{ID: c.ID, Class: "beginner", Skills: []model.SkillPoints{{Points: 1}}},
			{ID: c.ID, Class: "beginner", Skills: []model.SkillPoints{{ID: "strike"}}},
			{ID: c.ID, Class: "beginner", Skills: []model.SkillPoints{{ID: "strike", Points: 1}, {ID: "strike", Points: 2}}},
			{ID: c.ID, Class: "beginner", UnspentSkillPoints: -1},
		} {
			if err := svc.SaveCheckpoint(ctx, bad); !errors.Is(err, model.ErrBadSkill) {
				t.Fatalf("%+v: %v", bad, err)
			}
		}
		if store.chars[c.ID].MapID != "town" {
			t.Fatal("a refused checkpoint moved the character")
		}
	})

	t.Run("a checkpoint replaces the skill timers", func(t *testing.T) {
		end := time.Date(2026, 11, 12, 10, 0, 30, 0, time.UTC)
		cp := model.Checkpoint{ID: c.ID, Class: "beginner", MapID: "town", Level: 2, Timers: []model.SkillTimer{
			{Skill: "iron_body", Kind: model.CooldownTimer, Until: end.Add(30 * time.Second)},
			{Skill: "iron_body", Kind: model.BuffTimer, Until: end, Points: 3},
		}}
		if err := svc.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatal(err)
		}
		cp.Timers = cp.Timers[:1]
		if err := svc.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatal(err)
		}
		got, err := svc.Timers(ctx, c.ID)
		if err != nil || !reflect.DeepEqual(got, cp.Timers) {
			t.Fatalf("timers %+v (%v), want only the cooldown", got, err)
		}
	})

	t.Run("a checkpoint with a skill timer the table can't hold saves nothing", func(t *testing.T) {
		until := time.Now()
		for _, bad := range []model.Checkpoint{
			{ID: c.ID, Class: "beginner", Timers: []model.SkillTimer{{Kind: model.CooldownTimer, Until: until}}},
			{ID: c.ID, Class: "beginner", Timers: []model.SkillTimer{{Skill: "haste", Kind: "charge", Until: until}}},
			{ID: c.ID, Class: "beginner", Timers: []model.SkillTimer{{Skill: "haste", Kind: model.BuffTimer, Until: until}}},
			{ID: c.ID, Class: "beginner", Timers: []model.SkillTimer{{Skill: "haste", Kind: model.CooldownTimer, Until: until, Points: -1}}},
			{ID: c.ID, Class: "beginner", Timers: []model.SkillTimer{{Skill: "haste", Kind: model.CooldownTimer, Until: until}, {Skill: "haste", Kind: model.CooldownTimer, Until: until}}},
		} {
			if err := svc.SaveCheckpoint(ctx, bad); !errors.Is(err, model.ErrBadSkillTimer) {
				t.Fatalf("%+v: %v", bad, err)
			}
		}
		if store.chars[c.ID].MapID != "town" {
			t.Fatal("a refused checkpoint moved the character")
		}
	})

	t.Run("a checkpoint with a class the game data lacks saves nothing", func(t *testing.T) {
		before := store.chars[c.ID]
		for _, class := range []kernel.Class{"", "paladin"} {
			if err := svc.SaveCheckpoint(ctx, model.Checkpoint{ID: c.ID, Class: class, MapID: "cave"}); !errors.Is(err, model.ErrClassUnavailable) {
				t.Fatalf("class %q: %v", class, err)
			}
		}
		if store.chars[c.ID].MapID != before.MapID || store.chars[c.ID].Class != before.Class {
			t.Fatal("a refused checkpoint changed the character")
		}
	})

	t.Run("saving an unknown character is not_found", func(t *testing.T) {
		err := svc.SaveCheckpoint(ctx, model.Checkpoint{ID: kernel.NewCharacterID(), Class: "beginner"})
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("checkpoint: %v", err)
		}
		err = svc.SaveLevelUp(ctx, model.LevelUp{ID: kernel.NewCharacterID(), Level: 2})
		if !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("level-up: %v", err)
		}
	})
}

func TestDerivedForACharacter(t *testing.T) {
	svc, _, _ := newService(t)
	c, err := svc.Create(context.Background(), kernel.NewAccountID(), application.CreateInput{Name: "Ayla"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := svc.Derived(c)
	if err != nil || d.MaxHP != 135 || d.PhysicalAttack != 14 {
		t.Fatalf("derived = %+v, %v", d, err)
	}
	c.Points = gamedata.Stats{STR: 2, VIT: 3} // 2·1.5 more attack; 2·2 + 3·10 more HP
	if d, err := svc.Derived(c); err != nil || d.MaxHP != 169 || d.PhysicalAttack != 17 {
		t.Fatalf("derived with points = %+v, %v; want them on top of the starting stats", d, err)
	}
	if o := svc.Own(c); o != (gamedata.Stats{STR: 9, DEX: 7, INT: 7, AGI: 7, VIT: 10}) {
		t.Fatalf("own = %+v", o)
	}
	c.Class, c.Points = "warrior", gamedata.Stats{}
	if d, err := svc.Derived(c); err != nil || d.MaxHP != 144 {
		t.Fatalf("a warrior = %+v, %v; want the warrior's bases on the same starting stats", d, err)
	}
	c.Class = "paladin"
	if _, err := svc.Derived(c); !errors.Is(err, model.ErrClassUnavailable) {
		t.Fatalf("paladin: %v", err)
	}
}

// own is c's own stats, as the service gives them.
func own(svc *application.Service, c model.Character) gamedata.Stats {
	o := svc.Own(c)
	return o
}

func TestCharacterSlots(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newService(t)
	owner := kernel.NewAccountID()
	for _, name := range []string{"Ayla", "Robin", "Cass"} {
		if _, err := svc.Create(ctx, owner, application.CreateInput{Name: name}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	_, err := svc.Create(ctx, owner, application.CreateInput{Name: "Dori"})
	if !errors.Is(err, model.ErrSlotsFull) {
		t.Fatalf("fourth character: %v, want ErrSlotsFull (conflict)", err)
	}
	if _, err := svc.Create(ctx, kernel.NewAccountID(), application.CreateInput{Name: "Dori"}); err != nil {
		t.Fatal(err)
	}
}

// The character commits first; only then are its starter items granted, with retries.
func TestStarterItemsAfterTheCommit(t *testing.T) {
	ctx := context.Background()
	t.Run("the starter is asked once, after the commit, for the character and its account", func(t *testing.T) {
		starter := &fakeStarter{}
		svc, store, _ := newServiceWith(t, application.Deps{Starter: starter})
		starter.store = store
		owner := kernel.NewAccountID()
		c, err := svc.Create(ctx, owner, application.CreateInput{Name: "Ayla"})
		if err != nil {
			t.Fatal(err)
		}
		if len(starter.calls) != 1 || starter.calls[0] != (grant{owner, c.ID}) || !starter.committedFirst[0] {
			t.Fatalf("calls %+v, committed first %v", starter.calls, starter.committedFirst)
		}
	})
	// The retries wait on fake time.
	t.Run("a grant that fails twice is retried until it succeeds", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			starter := &fakeStarter{failures: 2, err: errors.New("item service unavailable"), retryable: true}
			svc, _, _ := newServiceWith(t, application.Deps{Starter: starter})
			if _, err := svc.Create(ctx, kernel.NewAccountID(), application.CreateInput{Name: "Ayla"}); err != nil {
				t.Fatal(err)
			}
			if len(starter.calls) != 3 {
				t.Fatalf("%d calls, want 3", len(starter.calls))
			}
		})
	})
	t.Run("a grant that never succeeds within 5 s still returns the committed character", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			starter := &fakeStarter{failures: 1 << 30, err: errors.New("item service unavailable"), retryable: true}
			svc, store, _ := newServiceWith(t, application.Deps{Starter: starter})
			start := time.Now()
			c, err := svc.Create(ctx, kernel.NewAccountID(), application.CreateInput{Name: "Ayla"})
			if err != nil {
				t.Fatalf("create failed although the character committed: %v", err)
			}
			if _, ok := store.chars[c.ID]; !ok || len(starter.calls) < 2 || time.Since(start) != 5*time.Second {
				t.Fatalf("character kept %v after %d calls in %v", ok, len(starter.calls), time.Since(start))
			}
		})
	})
	t.Run("a refusal by rule is not retried, and the character stays", func(t *testing.T) {
		starter := &fakeStarter{failures: 1 << 30, err: errors.New("unknown item"), retryable: false}
		svc, store, _ := newServiceWith(t, application.Deps{Starter: starter})
		c, err := svc.Create(ctx, kernel.NewAccountID(), application.CreateInput{Name: "Ayla"})
		if err != nil || len(starter.calls) != 1 {
			t.Fatalf("err %v after %d calls", err, len(starter.calls))
		}
		if _, ok := store.chars[c.ID]; !ok {
			t.Fatal("the character is gone")
		}
	})
	t.Run("with no starter wired nothing is granted", func(t *testing.T) {
		svc, _, _ := newService(t)
		if _, err := svc.Create(ctx, kernel.NewAccountID(), application.CreateInput{Name: "Ayla"}); err != nil {
			t.Fatal(err)
		}
	})
}
