package handler_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/sentinels"
	"finga/services/character/internal/character"
	"finga/services/character/internal/handler"
)

var (
	owner = kernel.NewAccountID()
	ayla  = character.Character{
		ID: kernel.NewCharacterID(), AccountID: owner, Name: "Ayla", Class: kernel.Warrior, Level: 3, Exp: 40,
		Stats: gamedata.Stats{STR: 18, DEX: 5, INT: 4, AGI: 5, VIT: 14}, UnspentPoints: 1, HP: 150, MP: 20,
		MapID: "forest", X: 412.5, Y: 608, UpdatedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
	}
)

// fakeCharacters records what the handler asked and answers err when set.
type fakeCharacters struct {
	created    character.CreateInput
	account    kernel.AccountID
	loaded     kernel.CharacterID
	checkpoint character.Checkpoint
	levelUp    character.LevelUp
	derivedErr error
	err        error
	calls      int
}

func (f *fakeCharacters) Create(_ context.Context, account kernel.AccountID, in character.CreateInput) (character.Character, error) {
	f.calls++
	f.account, f.created = account, in
	return ayla, f.err
}

func (f *fakeCharacters) List(_ context.Context, account kernel.AccountID) ([]character.Character, error) {
	f.calls++
	f.account = account
	return []character.Character{ayla}, f.err
}

func (f *fakeCharacters) Load(_ context.Context, id kernel.CharacterID, account kernel.AccountID) (character.Character, error) {
	f.calls++
	f.loaded, f.account = id, account
	return ayla, f.err
}

func (f *fakeCharacters) Derived(character.Character) (progression.Derived, error) {
	return progression.Derived{MaxHP: 250, MaxMP: 30}, f.derivedErr
}

func (f *fakeCharacters) SaveCheckpoint(_ context.Context, cp character.Checkpoint) error {
	f.calls++
	f.checkpoint = cp
	return f.err
}

func (f *fakeCharacters) SaveLevelUp(_ context.Context, lu character.LevelUp) error {
	f.calls++
	f.levelUp = lu
	return f.err
}

func client(t *testing.T, f *fakeCharacters) characterv1.CharacterServiceClient {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{Errors: handler.Errors, Logger: slog.New(slog.DiscardHandler)})
	characterv1.RegisterCharacterServiceServer(srv, handler.New(f))
	return characterv1.NewCharacterServiceClient(grpctest.Conn(t, srv))
}

func wantAyla(t *testing.T, c *characterv1.Character, withMax bool) {
	t.Helper()
	s := c.GetStats()
	if c.GetId() != ayla.ID.String() || c.GetAccountId() != owner.String() || c.GetName() != "Ayla" || c.GetClass() != "warrior" || c.GetLevel() != 3 || c.GetExp() != 40 ||
		s.GetStrength() != 18 || s.GetVitality() != 14 || c.GetUnspentPoints() != 1 || c.GetHp() != 150 || c.GetMp() != 20 ||
		c.GetMapId() != "forest" || c.GetX() != 412.5 || c.GetY() != 608 || !c.GetUpdatedAt().AsTime().Equal(ayla.UpdatedAt) {
		t.Fatalf("character %+v", c)
	}
	if withMax && (c.GetMaxHp() != 250 || c.GetMaxMp() != 30) {
		t.Fatalf("maximums %d %d", c.GetMaxHp(), c.GetMaxMp())
	}
}

func TestEachCallMapsItsFieldsAndResult(t *testing.T) {
	ctx := context.Background()
	f := &fakeCharacters{}
	c := client(t, f)
	t.Run("Create", func(t *testing.T) {
		res, err := c.Create(ctx, &characterv1.CreateRequest{AccountId: owner.String(), Name: "Ayla", Class: "warrior"})
		if err != nil || f.account != owner || f.created != (character.CreateInput{Name: "Ayla", Class: kernel.Warrior}) {
			t.Fatalf("input %+v %v, err %v", f.created, f.account, err)
		}
		wantAyla(t, res.GetCharacter(), true)
	})
	t.Run("List", func(t *testing.T) {
		res, err := c.List(ctx, &characterv1.ListRequest{AccountId: owner.String()})
		if err != nil || len(res.GetCharacters()) != 1 {
			t.Fatalf("list %+v %v", res, err)
		}
		wantAyla(t, res.GetCharacters()[0], true)
	})
	t.Run("Load carries the owner and the maximums", func(t *testing.T) {
		res, err := c.Load(ctx, &characterv1.LoadRequest{CharacterId: ayla.ID.String(), AccountId: owner.String()})
		if err != nil || f.loaded != ayla.ID || f.account != owner {
			t.Fatalf("loaded %v for %v, err %v", f.loaded, f.account, err)
		}
		wantAyla(t, res.GetCharacter(), true)
	})
	t.Run("SaveCheckpoint", func(t *testing.T) {
		if _, err := c.SaveCheckpoint(ctx, &characterv1.SaveCheckpointRequest{CharacterId: ayla.ID.String(), MapId: "cave", X: 10, Y: 20, Hp: 5, Mp: 6, Exp: 7}); err != nil {
			t.Fatal(err)
		}
		if f.checkpoint != (character.Checkpoint{ID: ayla.ID, MapID: "cave", X: 10, Y: 20, HP: 5, MP: 6, Exp: 7}) {
			t.Fatalf("checkpoint %+v", f.checkpoint)
		}
	})
	t.Run("SaveLevelUp", func(t *testing.T) {
		stats := &characterv1.Stats{Strength: 21, Dexterity: 5, Intelligence: 4, Agility: 5, Vitality: 16}
		if _, err := c.SaveLevelUp(ctx, &characterv1.SaveLevelUpRequest{CharacterId: ayla.ID.String(), Level: 4, Exp: 3, Stats: stats, Hp: 280, Mp: 31}); err != nil {
			t.Fatal(err)
		}
		want := character.LevelUp{ID: ayla.ID, Level: 4, Exp: 3, Stats: gamedata.Stats{STR: 21, DEX: 5, INT: 4, AGI: 5, VIT: 16}, HP: 280, MP: 31}
		if f.levelUp != want {
			t.Fatalf("level-up %+v", f.levelUp)
		}
	})
}

func TestLoadFailsWhenTheStatsCannotBeDerived(t *testing.T) {
	c := client(t, &fakeCharacters{derivedErr: character.ErrClassUnavailable})
	_, err := c.Load(context.Background(), &characterv1.LoadRequest{CharacterId: ayla.ID.String(), AccountId: owner.String()})
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(characterv1.Reason_REASON_CLASS_UNAVAILABLE) {
		t.Fatalf("got %v", err)
	}
}

func TestAMalformedIDNeverReachesTheModule(t *testing.T) {
	ctx := context.Background()
	f := &fakeCharacters{}
	c := client(t, f)
	calls := []func() error{
		func() error {
			_, err := c.Create(ctx, &characterv1.CreateRequest{AccountId: "x", Name: "A", Class: "warrior"})
			return err
		},
		func() error { _, err := c.List(ctx, &characterv1.ListRequest{}); return err },
		func() error {
			_, err := c.Load(ctx, &characterv1.LoadRequest{CharacterId: "nope", AccountId: owner.String()})
			return err
		},
		func() error {
			_, err := c.SaveCheckpoint(ctx, &characterv1.SaveCheckpointRequest{CharacterId: "?"})
			return err
		},
		func() error {
			_, err := c.SaveLevelUp(ctx, &characterv1.SaveLevelUpRequest{CharacterId: ""})
			return err
		},
	}
	for i, call := range calls {
		if r, ok := grpcx.ReasonOf(call()); !ok || r != grpcx.Key(characterv1.Reason_REASON_BAD_ID) {
			t.Fatalf("call %d: %+v", i, r)
		}
	}
	if f.calls != 0 {
		t.Fatalf("the module was called %d times", f.calls)
	}
}

func TestEveryModuleErrorArrivesWithItsReason(t *testing.T) {
	for _, row := range handler.Errors {
		t.Run(grpcx.Key(row.Reason).Name, func(t *testing.T) {
			c := client(t, &fakeCharacters{err: row.Err})
			_, err := c.Load(context.Background(), &characterv1.LoadRequest{CharacterId: ayla.ID.String(), AccountId: owner.String()})
			if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(row.Reason) || status.Code(err) != row.Code {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("someone else's character is not found", func(t *testing.T) {
		c := client(t, &fakeCharacters{err: character.ErrNotFound})
		_, err := c.Load(context.Background(), &characterv1.LoadRequest{CharacterId: ayla.ID.String(), AccountId: kernel.NewAccountID().String()})
		if status.Code(err) != codes.NotFound {
			t.Fatalf("got %v", err)
		}
	})
}

func TestTheTableCoversTheModuleAndTheContract(t *testing.T) {
	declared, err := sentinels.Declared(filepath.Join("..", "character"))
	if err != nil || len(declared) == 0 {
		t.Fatalf("declared %v, err %v", declared, err)
	}
	mapped, err := sentinels.Referenced("errors.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range declared {
		if !mapped[name] {
			t.Errorf("%s has no row in Errors", name)
		}
	}
	used := map[grpcx.Reason]bool{}
	for _, row := range handler.Errors {
		used[grpcx.Key(row.Reason)] = true
	}
	for _, r := range grpcx.Reasons(characterv1.Reason(0).Descriptor()) {
		if !used[r] {
			t.Errorf("%s has no row in Errors", r)
		}
	}
}
