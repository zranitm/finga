package character_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"finga/internal/application/character"
	"finga/internal/application/gamedata"
	"finga/internal/model"
	"finga/pkg/errs"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newService(t *testing.T) (*character.Service, *fakeStore, *gamedata.Data) {
	t.Helper()
	data := loadData(t)
	store := newFakeStore()
	c := &clock{t: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
	return character.NewService(store, data, character.Options{Now: c.now}), store, data
}

func TestCreate(t *testing.T) {
	ctx := context.Background()
	owner := model.NewAccountID()

	t.Run("valid", func(t *testing.T) {
		svc, store, data := newService(t)
		c, err := svc.Create(ctx, owner, character.CreateInput{Name: "Ayla", Class: model.Warrior})
		if err != nil {
			t.Fatal(err)
		}
		town := data.Town()
		warrior, _ := data.Class(model.Warrior)
		checks := []struct {
			name string
			ok   bool
		}{
			{"owned by the account", c.AccountID == owner},
			{"name kept as given", c.Name == "Ayla"},
			{"level 1 with no EXP", c.Level == 1 && c.Exp == 0},
			{"base stats of the class", c.Stats == warrior.BaseStats},
			{"no unspent points", c.UnspentPoints == 0},
			{"HP and MP full: 204 and 28 for a level 1 warrior", c.HP == 204 && c.MP == 28},
			{"starts in the town at its player spawn", c.MapID == town.ID && c.X == town.PlayerSpawn.X && c.Y == town.PlayerSpawn.Y},
			{"stored", len(store.chars) == 1},
		}
		for _, ck := range checks {
			if !ck.ok {
				t.Fatalf("%s: %+v", ck.name, c)
			}
		}
	})

	t.Run("a bowman gets its own base stats and HP", func(t *testing.T) {
		svc, _, _ := newService(t)
		c, err := svc.Create(ctx, owner, character.CreateInput{Name: "Robin", Class: model.Bowman})
		if err != nil {
			t.Fatal(err)
		}
		if c.Stats != (gamedata.Stats{STR: 5, DEX: 12, INT: 4, AGI: 10, VIT: 5}) || c.HP != 105 || c.MP != 33 {
			t.Fatalf("bowman = %+v", c)
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
				_, err := svc.Create(ctx, owner, character.CreateInput{Name: tt.input, Class: model.Warrior})
				if !errors.Is(err, character.ErrBadName) || errs.CodeOf(err) != errs.InvalidArgument {
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
			if _, err := svc.Create(ctx, owner, character.CreateInput{Name: name, Class: model.Warrior}); err != nil {
				t.Fatalf("%q: %v", name, err)
			}
		}
	})

	t.Run("classes not in Stage 1 are rejected", func(t *testing.T) {
		for _, class := range []model.Class{model.Thief, model.Magician, "paladin"} {
			t.Run(string(class), func(t *testing.T) {
				svc, _, _ := newService(t)
				_, err := svc.Create(ctx, owner, character.CreateInput{Name: "Ayla", Class: class})
				if !errors.Is(err, character.ErrClassUnavailable) || errs.CodeOf(err) != errs.InvalidArgument {
					t.Fatalf("err = %v", err)
				}
				if strings.Contains(errs.MessageOf(err), string(class)) {
					t.Fatal("message echoes the input")
				}
			})
		}
	})

	t.Run("a duplicate name becomes conflict", func(t *testing.T) {
		svc, _, _ := newService(t)
		if _, err := svc.Create(ctx, owner, character.CreateInput{Name: "Ayla", Class: model.Warrior}); err != nil {
			t.Fatal(err)
		}
		_, err := svc.Create(ctx, model.NewAccountID(), character.CreateInput{Name: "AYLA", Class: model.Bowman})
		if !errors.Is(err, character.ErrNameTaken) || errs.CodeOf(err) != errs.Conflict {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestListAndLoad(t *testing.T) {
	ctx := context.Background()
	svc, _, _ := newService(t)
	alice, bob := model.NewAccountID(), model.NewAccountID()
	a1, _ := svc.Create(ctx, alice, character.CreateInput{Name: "Ayla", Class: model.Warrior})
	a2, _ := svc.Create(ctx, alice, character.CreateInput{Name: "Robin", Class: model.Bowman})
	if _, err := svc.Create(ctx, bob, character.CreateInput{Name: "Bruno", Class: model.Warrior}); err != nil {
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
		empty, err := svc.List(ctx, model.NewAccountID())
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
		if !errors.Is(err, character.ErrNotFound) || errs.CodeOf(err) != errs.NotFound {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("an unknown ID gives not_found", func(t *testing.T) {
		_, err := svc.Load(ctx, model.NewCharacterID(), alice)
		if !errors.Is(err, character.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestSaves(t *testing.T) {
	ctx := context.Background()
	svc, store, _ := newService(t)
	owner := model.NewAccountID()
	c, err := svc.Create(ctx, owner, character.CreateInput{Name: "Ayla", Class: model.Warrior})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a checkpoint updates position, HP, MP and EXP", func(t *testing.T) {
		cp := character.Checkpoint{ID: c.ID, MapID: "forest", X: 300, Y: 608, HP: 150, MP: 20, Exp: 12}
		if err := svc.SaveCheckpoint(ctx, cp); err != nil {
			t.Fatal(err)
		}
		got := store.chars[c.ID]
		if got.MapID != "forest" || got.X != 300 || got.Y != 608 || got.HP != 150 || got.MP != 20 || got.Exp != 12 || got.Level != 1 {
			t.Fatalf("after checkpoint: %+v", got)
		}
		if !got.UpdatedAt.After(c.UpdatedAt) && !got.UpdatedAt.Equal(c.UpdatedAt) {
			t.Fatal("updated_at went backwards")
		}
	})

	t.Run("a level-up updates level, EXP, stats, HP and MP", func(t *testing.T) {
		lu := character.LevelUp{ID: c.ID, Level: 2, Exp: 3, Stats: gamedata.Stats{STR: 15, DEX: 5, INT: 4, AGI: 5, VIT: 12}, HP: 250, MP: 32}
		if err := svc.SaveLevelUp(ctx, lu); err != nil {
			t.Fatal(err)
		}
		got := store.chars[c.ID]
		if got.Level != 2 || got.Exp != 3 || got.Stats != lu.Stats || got.HP != 250 || got.MP != 32 {
			t.Fatalf("after level-up: %+v", got)
		}
	})

	t.Run("saving an unknown character is not_found", func(t *testing.T) {
		err := svc.SaveCheckpoint(ctx, character.Checkpoint{ID: model.NewCharacterID()})
		if !errors.Is(err, character.ErrNotFound) {
			t.Fatalf("checkpoint: %v", err)
		}
		err = svc.SaveLevelUp(ctx, character.LevelUp{ID: model.NewCharacterID(), Level: 2})
		if !errors.Is(err, character.ErrNotFound) {
			t.Fatalf("level-up: %v", err)
		}
	})
}

func TestDerivedForACharacter(t *testing.T) {
	svc, _, _ := newService(t)
	c, err := svc.Create(context.Background(), model.NewAccountID(), character.CreateInput{Name: "Ayla", Class: model.Warrior})
	if err != nil {
		t.Fatal(err)
	}
	d, err := svc.Derived(c)
	if err != nil || d.MaxHP != 204 || d.AttackPower != 21 {
		t.Fatalf("derived = %+v, %v", d, err)
	}
	c.Class = model.Thief
	if _, err := svc.Derived(c); !errors.Is(err, character.ErrClassUnavailable) {
		t.Fatalf("thief: %v", err)
	}
}
