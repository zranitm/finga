package handler_test

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/sentineltest"
	"finga/pkg/transient"
	"finga/services/character/internal/application"
	"finga/services/character/internal/handler"
	"finga/services/character/internal/model"
)

var (
	owner = kernel.NewAccountID()
	ayla  = model.Character{
		ID: kernel.NewCharacterID(), AccountID: owner, Name: "Ayla", Class: kernel.Warrior, Level: 3, Exp: 40,
		Points: gamedata.Stats{STR: 6, VIT: 3}, UnspentPoints: 1, FreeResetUsed: true, HP: 150, MP: 20,
		MapID: "forest", X: 412.5, Y: 608, UpdatedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
	}
)

type fakeCharacters struct {
	created    application.CreateInput
	account    kernel.AccountID
	loaded     kernel.CharacterID
	quests     []model.Quest
	checkpoint model.Checkpoint
	levelUp    model.LevelUp
	derivedErr error
	err        error
	calls      int
}

func (f *fakeCharacters) Create(_ context.Context, account kernel.AccountID, in application.CreateInput) (model.Character, error) {
	f.calls++
	f.account, f.created = account, in
	return ayla, f.err
}

func (f *fakeCharacters) List(_ context.Context, account kernel.AccountID) ([]model.Character, error) {
	f.calls++
	f.account = account
	return []model.Character{ayla}, f.err
}

func (f *fakeCharacters) Load(_ context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	f.calls++
	f.loaded, f.account = id, account
	return ayla, f.err
}

func (f *fakeCharacters) Quests(_ context.Context, id kernel.CharacterID) ([]model.Quest, error) {
	f.calls++
	f.loaded = id
	return f.quests, f.err
}

func (f *fakeCharacters) Own(model.Character) (gamedata.Stats, error) {
	return gamedata.Stats{STR: 18, DEX: 5, INT: 4, AGI: 5, VIT: 13}, f.derivedErr
}

func (f *fakeCharacters) Derived(model.Character) (progression.Derived, error) {
	return progression.Derived{MaxHP: 250, MaxMP: 30}, f.derivedErr
}

func (f *fakeCharacters) SaveCheckpoint(_ context.Context, cp model.Checkpoint) error {
	f.calls++
	f.checkpoint = cp
	return f.err
}

func (f *fakeCharacters) SaveLevelUp(_ context.Context, lu model.LevelUp) error {
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
	pts, own := c.GetPoints(), c.GetOwnStats()
	if c.GetId() != ayla.ID.String() || c.GetAccountId() != owner.String() || c.GetName() != "Ayla" || c.GetClass() != "warrior" || c.GetLevel() != 3 || c.GetExp() != 40 ||
		pts.GetStrength() != 6 || pts.GetVitality() != 3 || c.GetUnspentPoints() != 1 || !c.GetFreeResetUsed() || c.GetHp() != 150 || c.GetMp() != 20 ||
		c.GetMapId() != "forest" || c.GetX() != 412.5 || c.GetY() != 608 || !c.GetUpdatedAt().AsTime().Equal(ayla.UpdatedAt) {
		t.Fatalf("character %+v", c)
	}
	if withMax && (c.GetMaxHp() != 250 || c.GetMaxMp() != 30 || own.GetStrength() != 18 || own.GetVitality() != 13) {
		t.Fatalf("maximums %d %d, own stats %v", c.GetMaxHp(), c.GetMaxMp(), own)
	}
}

func TestEachCallMapsItsFieldsAndResult(t *testing.T) {
	ctx := context.Background()
	f := &fakeCharacters{}
	c := client(t, f)
	t.Run("Create", func(t *testing.T) {
		res, err := c.Create(ctx, &characterv1.CreateRequest{AccountId: owner.String(), Name: "Ayla", Class: "warrior"})
		if err != nil || f.account != owner || f.created != (application.CreateInput{Name: "Ayla", Class: kernel.Warrior}) {
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
	t.Run("Load carries the owner, the maximums and the quests", func(t *testing.T) {
		f.quests = []model.Quest{{ID: "slime_cleanup", Status: model.QuestActive, Progress: []int{7}}, {ID: "sticky_samples", Status: model.QuestFinished, TimesFinished: 2}}
		defer func() { f.quests = nil }()
		res, err := c.Load(ctx, &characterv1.LoadRequest{CharacterId: ayla.ID.String(), AccountId: owner.String()})
		if err != nil || f.loaded != ayla.ID || f.account != owner {
			t.Fatalf("loaded %v for %v, err %v", f.loaded, f.account, err)
		}
		wantAyla(t, res.GetCharacter(), true)
		want := []*characterv1.Quest{
			{QuestId: "slime_cleanup", Status: characterv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{7}},
			{QuestId: "sticky_samples", Status: characterv1.QuestStatus_QUEST_STATUS_FINISHED, Progress: []int32{}, TimesFinished: 2},
		}
		got := res.GetQuests()
		if len(got) != len(want) {
			t.Fatalf("quests %v", got)
		}
		for k := range want {
			if !proto.Equal(got[k], want[k]) {
				t.Fatalf("quest %d: %v, want %v", k, got[k], want[k])
			}
		}
	})
	t.Run("SaveCheckpoint", func(t *testing.T) {
		points := &characterv1.Stats{Strength: 9, Dexterity: 1, Intelligence: 2, Agility: 3, Vitality: 4}
		quests := []*characterv1.Quest{
			{QuestId: "slime_cleanup", Status: characterv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{3, 0}, TimesFinished: 1},
			{QuestId: "sticky_samples", Status: characterv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 1},
			{QuestId: "odd", Status: characterv1.QuestStatus(9)},
		}
		if _, err := c.SaveCheckpoint(ctx, &characterv1.SaveCheckpointRequest{CharacterId: ayla.ID.String(), MapId: "cave", X: 10, Y: 20, Hp: 5, Mp: 6, Level: 5, Exp: 7, Points: points, UnspentPoints: 1, FreeResetUsed: true,
			Quests: quests}); err != nil {
			t.Fatal(err)
		}
		want := model.Checkpoint{ID: ayla.ID, MapID: "cave", X: 10, Y: 20, HP: 5, MP: 6, Level: 5, Exp: 7, Points: gamedata.Stats{STR: 9, DEX: 1, INT: 2, AGI: 3, VIT: 4}, UnspentPoints: 1, FreeResetUsed: true,
			Quests: []model.Quest{
				{ID: "slime_cleanup", Status: model.QuestActive, Progress: []int{3, 0}, TimesFinished: 1},
				{ID: "sticky_samples", Status: model.QuestFinished, TimesFinished: 1},
				{ID: "odd"}, // an unknown status reaches the module empty, which refuses it
			},
		}
		if !reflect.DeepEqual(f.checkpoint, want) {
			t.Fatalf("checkpoint %+v", f.checkpoint)
		}
	})
	t.Run("SaveLevelUp", func(t *testing.T) {
		points := &characterv1.Stats{Strength: 9, Vitality: 6}
		if _, err := c.SaveLevelUp(ctx, &characterv1.SaveLevelUpRequest{CharacterId: ayla.ID.String(), Level: 4, Exp: 3, Points: points, Hp: 280, Mp: 31}); err != nil {
			t.Fatal(err)
		}
		want := model.LevelUp{ID: ayla.ID, Level: 4, Exp: 3, Points: gamedata.Stats{STR: 9, VIT: 6}, HP: 280, MP: 31}
		if f.levelUp != want {
			t.Fatalf("level-up %+v", f.levelUp)
		}
	})
}

func TestLoadFailsWhenTheStatsCannotBeDerived(t *testing.T) {
	c := client(t, &fakeCharacters{derivedErr: model.ErrClassUnavailable})
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
		t.Run(row.Err.Error(), func(t *testing.T) {
			c := client(t, &fakeCharacters{err: row.Err})
			_, err := c.Load(context.Background(), &characterv1.LoadRequest{CharacterId: ayla.ID.String(), AccountId: owner.String()})
			want := grpcx.Reason{}
			if row.Reason != nil {
				want = grpcx.Key(row.Reason)
			}
			if r, ok := grpcx.ReasonOf(err); ok != (row.Reason != nil) || r != want || status.Code(err) != row.Code {
				t.Fatalf("got %v", err)
			}
		})
	}
	t.Run("someone else's character is not found", func(t *testing.T) {
		c := client(t, &fakeCharacters{err: model.ErrNotFound})
		_, err := c.Load(context.Background(), &characterv1.LoadRequest{CharacterId: ayla.ID.String(), AccountId: kernel.NewAccountID().String()})
		if status.Code(err) != codes.NotFound {
			t.Fatalf("got %v", err)
		}
	})
}

// A store that timed out answers Unavailable with no reason: a caller retries what carries no rule.
func TestATimeoutAsksTheCallerToRetry(t *testing.T) {
	c := client(t, &fakeCharacters{err: fmt.Errorf("list: %w", transient.ErrUnavailable)})
	_, err := c.List(context.Background(), &characterv1.ListRequest{AccountId: owner.String()})
	if status.Code(err) != codes.Unavailable || grpcx.IsRule(err) {
		t.Fatalf("got %v, want Unavailable without a reason", err)
	}
}

func TestTheTableCoversTheModuleAndTheContract(t *testing.T) {
	declared, err := sentineltest.Declared(filepath.Join("..", "model"))
	if err != nil || len(declared) == 0 {
		t.Fatalf("declared %v, err %v", declared, err)
	}
	mapped, err := sentineltest.Referenced("errors.go")
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
		if row.Reason != nil {
			used[grpcx.Key(row.Reason)] = true
		}
	}
	for _, r := range grpctest.Reasons(characterv1.Reason(0).Descriptor()) {
		if !used[r] {
			t.Errorf("%s has no row in Errors", r)
		}
	}
}
