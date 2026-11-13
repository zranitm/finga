package handler_test

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/transient"
	"finga/services/character"
	"finga/services/character/internal/handler"
)

var (
	owner = kernel.NewAccountID()
	ayla  = character.Character{
		ID: kernel.NewCharacterID(), AccountID: owner, Name: "Ayla", Class: kernel.Class("warrior"), Level: 3, Exp: 40,
		Points: gamedata.Stats{STR: 6, VIT: 3}, UnspentPoints: 1, FreeResetUsed: true, OwnStats: gamedata.Stats{STR: 18, DEX: 5, INT: 4, AGI: 5, VIT: 13}, UnspentSkillPoints: 4,
		HP: 150, MaxHP: 250, MP: 20, MaxMP: 30, MapID: "forest", X: 412.5, Y: 608, UpdatedAt: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC), SkillBar: []string{"strike", "", "", "spark", "", ""},
	}
)

// fakeCharacters records what the handler asked and answers err when set.
type fakeCharacters struct {
	name       string
	account    kernel.AccountID
	loaded     kernel.CharacterID
	quests     []character.Quest
	skills     []character.SkillPoints
	timers     []character.SkillTimer
	checkpoint character.Checkpoint
	skillBar   []string
	levelUp    character.LevelUp
	err        error
	calls      int
}

func (f *fakeCharacters) Create(_ context.Context, account kernel.AccountID, name string) (character.Character, error) {
	f.calls++
	f.account, f.name = account, name
	return ayla, f.err
}

func (f *fakeCharacters) List(_ context.Context, account kernel.AccountID) ([]character.Character, error) {
	f.calls++
	f.account = account
	return []character.Character{ayla}, f.err
}

func (f *fakeCharacters) Load(_ context.Context, id kernel.CharacterID, account kernel.AccountID) (character.Loaded, error) {
	f.calls++
	f.loaded, f.account = id, account
	return character.Loaded{Character: ayla, Quests: f.quests, Skills: f.skills, Timers: f.timers}, f.err
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

func (f *fakeCharacters) SetSkillBar(_ context.Context, id kernel.CharacterID, account kernel.AccountID, bar []string) error {
	f.calls++
	f.loaded, f.account, f.skillBar = id, account, bar
	return f.err
}

func client(t *testing.T, f *fakeCharacters) characterv1.CharacterServiceClient {
	t.Helper()
	srv := grpcx.NewServer(grpcx.ServerOptions{Errors: handler.Errors, Logger: slog.New(slog.DiscardHandler)})
	characterv1.RegisterCharacterServiceServer(srv, handler.New(f))
	return characterv1.NewCharacterServiceClient(grpctest.Conn(t, srv))
}

func wantAyla(t *testing.T, c *characterv1.Character) {
	t.Helper()
	pts, own := c.GetPoints(), c.GetOwnStats()
	if c.GetId() != ayla.ID.String() || c.GetAccountId() != owner.String() || c.GetName() != "Ayla" || c.GetClass() != "warrior" || c.GetLevel() != 3 || c.GetExp() != 40 ||
		pts.GetStrength() != 6 || pts.GetVitality() != 3 || c.GetUnspentPoints() != 1 || !c.GetFreeResetUsed() || c.GetHp() != 150 || c.GetMp() != 20 ||
		c.GetMapId() != "forest" || c.GetX() != 412.5 || c.GetY() != 608 || !c.GetUpdatedAt().AsTime().Equal(ayla.UpdatedAt) || !slices.Equal(c.GetSkillBar(), ayla.SkillBar) {
		t.Fatalf("character %+v", c)
	}
	if c.GetMaxHp() != 250 || c.GetMaxMp() != 30 || own.GetStrength() != 18 || own.GetVitality() != 13 {
		t.Fatalf("maximums %d %d, own stats %v", c.GetMaxHp(), c.GetMaxMp(), own)
	}
}

func TestEachCallMapsItsFieldsAndResult(t *testing.T) {
	ctx := context.Background()
	f := &fakeCharacters{}
	c := client(t, f)
	t.Run("Create", func(t *testing.T) {
		res, err := c.Create(ctx, &characterv1.CreateRequest{AccountId: owner.String(), Name: "Ayla"})
		if err != nil || f.account != owner || f.name != "Ayla" {
			t.Fatalf("input %q %v, err %v", f.name, f.account, err)
		}
		wantAyla(t, res.GetCharacter())
	})
	t.Run("List", func(t *testing.T) {
		res, err := c.List(ctx, &characterv1.ListRequest{AccountId: owner.String()})
		if err != nil || len(res.GetCharacters()) != 1 {
			t.Fatalf("list %+v %v", res, err)
		}
		wantAyla(t, res.GetCharacters()[0])
	})
	t.Run("Load carries the owner, the maximums, the quests, the skills and the skill timers", func(t *testing.T) {
		f.quests = []character.Quest{{ID: "slime_cleanup", Status: character.QuestActive, Progress: []int{7}}, {ID: "sticky_samples", Status: character.QuestFinished, TimesFinished: 2}}
		f.skills = []character.SkillPoints{{ID: "second_wind", Points: 2}, {ID: "strike", Points: 11}}
		end := time.Date(2026, 11, 12, 10, 0, 30, 0, time.UTC)
		f.timers = []character.SkillTimer{{Skill: "iron_body", Kind: character.BuffTimer, Until: end, Points: 3}}
		defer func() { f.quests, f.skills, f.timers = nil, nil, nil }()
		res, err := c.Load(ctx, &characterv1.LoadRequest{CharacterId: ayla.ID.String(), AccountId: owner.String()})
		if err != nil || f.loaded != ayla.ID || f.account != owner {
			t.Fatalf("loaded %v for %v, err %v", f.loaded, f.account, err)
		}
		wantAyla(t, res.GetCharacter())
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
		skills := res.GetSkills()
		if len(skills) != 2 || !proto.Equal(skills[1], &characterv1.SkillPoints{SkillId: "strike", Points: 11}) || res.GetCharacter().GetUnspentSkillPoints() != 4 {
			t.Fatalf("skills %v, unspent %d", skills, res.GetCharacter().GetUnspentSkillPoints())
		}
		buff := &characterv1.SkillTimer{SkillId: "iron_body", Kind: characterv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF, Until: timestamppb.New(end), Points: 3}
		if timers := res.GetTimers(); len(timers) != 1 || !proto.Equal(timers[0], buff) {
			t.Fatalf("timers %v, want Iron Body's buff", timers)
		}
	})
	t.Run("SaveCheckpoint", func(t *testing.T) {
		points := &characterv1.Stats{Strength: 9, Dexterity: 1, Intelligence: 2, Agility: 3, Vitality: 4}
		quests := []*characterv1.Quest{
			{QuestId: "slime_cleanup", Status: characterv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{3, 0}, TimesFinished: 1},
			{QuestId: "sticky_samples", Status: characterv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 1},
			{QuestId: "odd", Status: characterv1.QuestStatus(9)},
		}
		if _, err := c.SaveCheckpoint(ctx, &characterv1.SaveCheckpointRequest{CharacterId: ayla.ID.String(), Class: "thief", MapId: "cave", X: 10, Y: 20, Hp: 5, Mp: 6, Level: 5, Exp: 7, Points: points, UnspentPoints: 1, FreeResetUsed: true,
			Quests: quests, UnspentSkillPoints: 3, Skills: []*characterv1.SkillPoints{{SkillId: "spark", Points: 6}},
			Timers: []*characterv1.SkillTimer{
				{SkillId: "haste", Kind: characterv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN, Until: timestamppb.New(time.Unix(1_800_000_000, 0))},
				{SkillId: "haste", Kind: characterv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF, Until: timestamppb.New(time.Unix(1_799_999_990, 0)), Points: 2},
				{SkillId: "odd", Kind: characterv1.SkillTimerKind(9), Until: timestamppb.New(time.Unix(1_800_000_000, 0))},
			}}); err != nil {
			t.Fatal(err)
		}
		want := character.Checkpoint{ID: ayla.ID, Class: "thief", MapID: "cave", X: 10, Y: 20, HP: 5, MP: 6, Level: 5, Exp: 7, Points: gamedata.Stats{STR: 9, DEX: 1, INT: 2, AGI: 3, VIT: 4}, UnspentPoints: 1, FreeResetUsed: true,
			Quests: []character.Quest{
				{ID: "slime_cleanup", Status: character.QuestActive, Progress: []int{3, 0}, TimesFinished: 1},
				{ID: "sticky_samples", Status: character.QuestFinished, TimesFinished: 1},
				{ID: "odd"}, // an unknown status reaches the module empty, which refuses it
			},
			UnspentSkillPoints: 3, Skills: []character.SkillPoints{{ID: "spark", Points: 6}},
			Timers: []character.SkillTimer{
				{Skill: "haste", Kind: character.CooldownTimer, Until: time.Unix(1_800_000_000, 0).UTC()},
				{Skill: "haste", Kind: character.BuffTimer, Until: time.Unix(1_799_999_990, 0).UTC(), Points: 2},
				{Skill: "odd", Until: time.Unix(1_800_000_000, 0).UTC()}, // an unknown kind reaches the module empty, which refuses it
			},
		}
		if !reflect.DeepEqual(f.checkpoint, want) {
			t.Fatalf("checkpoint %+v", f.checkpoint)
		}
	})
	t.Run("SetSkillBar", func(t *testing.T) {
		slots := []string{"", "throw", "", "", "", "strike"}
		if _, err := c.SetSkillBar(ctx, &characterv1.SetSkillBarRequest{CharacterId: ayla.ID.String(), AccountId: owner.String(), Slots: slots}); err != nil {
			t.Fatal(err)
		}
		if f.loaded != ayla.ID || f.account != owner || !slices.Equal(f.skillBar, slots) {
			t.Fatalf("set %v's bar to %q for %v", f.loaded, f.skillBar, f.account)
		}
	})
	t.Run("SaveLevelUp", func(t *testing.T) {
		points := &characterv1.Stats{Strength: 9, Vitality: 6}
		if _, err := c.SaveLevelUp(ctx, &characterv1.SaveLevelUpRequest{CharacterId: ayla.ID.String(), Level: 4, Exp: 3, Points: points, Hp: 280, Mp: 31}); err != nil {
			t.Fatal(err)
		}
		want := character.LevelUp{ID: ayla.ID, Level: 4, Exp: 3, Points: gamedata.Stats{STR: 9, VIT: 6}, HP: 280, MP: 31}
		if f.levelUp != want {
			t.Fatalf("level-up %+v", f.levelUp)
		}
	})
}

func TestAMalformedIDNeverReachesTheModule(t *testing.T) {
	ctx := context.Background()
	f := &fakeCharacters{}
	c := client(t, f)
	calls := []func() error{
		func() error {
			_, err := c.Create(ctx, &characterv1.CreateRequest{AccountId: "x", Name: "A"})
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
		func() error {
			_, err := c.SetSkillBar(ctx, &characterv1.SetSkillBarRequest{CharacterId: ayla.ID.String(), AccountId: "x"})
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

// Each kernel error the facade returns reaches the caller with its code and reason.
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
		c := client(t, &fakeCharacters{err: kernel.ErrCharacterNotFound})
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
