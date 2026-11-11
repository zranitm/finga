package clients_test

import (
	"context"
	"reflect"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/grpcx/grpctest"
	"finga/services/world/internal/clients"
	"finga/services/world/internal/model"
)

type characters struct {
	characterv1.UnimplementedCharacterServiceServer
	checkpoints []*characterv1.SaveCheckpointRequest
}

func (f *characters) SaveCheckpoint(_ context.Context, req *characterv1.SaveCheckpointRequest) (*characterv1.SaveCheckpointResponse, error) {
	f.checkpoints = append(f.checkpoints, req)
	return &characterv1.SaveCheckpointResponse{}, nil
}

func TestSaverOverTheCharacterService(t *testing.T) {
	f := &characters{}
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	characterv1.RegisterCharacterServiceServer(srv, f)
	s := clients.NewSaver(characterv1.NewCharacterServiceClient(grpctest.Conn(t, srv)))
	id := kernel.NewCharacterID()
	ctx := context.Background()
	log := model.Quests{
		"sticky_samples": {Status: model.QuestActive, Progress: []int{3}, TimesFinished: 1},
		"slime_cleanup":  {Status: model.QuestFinished, TimesFinished: 1},
	}
	if err := s.SaveCheckpoint(ctx, model.Checkpoint{ID: id, MapID: "cave", X: 1.5, Y: 2, HP: 3, MP: 4, Level: 4, Exp: 5, Points: model.Points{Spent: gamedata.Stats{STR: 9, VIT: 4}, Unspent: 2, FreeResetUsed: true}, Quests: log,
		Skills: model.Skills{Points: map[string]int{"throw": 7, "second_wind": 2}, Unspent: 3}}); err != nil {
		t.Fatal(err)
	}
	want := []*characterv1.Quest{
		{QuestId: "slime_cleanup", Status: characterv1.QuestStatus_QUEST_STATUS_FINISHED, Progress: []int32{}, TimesFinished: 1},
		{QuestId: "sticky_samples", Status: characterv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{3}, TimesFinished: 1},
	}
	if got := f.checkpoints[0].GetQuests(); len(got) != 2 || !proto.Equal(got[0], want[0]) || !proto.Equal(got[1], want[1]) {
		t.Fatalf("quests %v, want the whole log by ID", got)
	}
	if cp := f.checkpoints[0]; cp.GetCharacterId() != id.String() || cp.GetMapId() != "cave" || cp.GetX() != 1.5 || cp.GetY() != 2 || cp.GetHp() != 3 || cp.GetMp() != 4 ||
		cp.GetLevel() != 4 || cp.GetExp() != 5 || cp.GetPoints().GetStrength() != 9 || cp.GetPoints().GetVitality() != 4 || cp.GetUnspentPoints() != 2 || !cp.GetFreeResetUsed() {
		t.Fatalf("checkpoint %+v", cp)
	}
	if got := f.checkpoints[0].GetSkills(); f.checkpoints[0].GetUnspentSkillPoints() != 3 || len(got) != 2 ||
		!proto.Equal(got[0], &characterv1.SkillPoints{SkillId: "second_wind", Points: 2}) || !proto.Equal(got[1], &characterv1.SkillPoints{SkillId: "throw", Points: 7}) {
		t.Fatalf("skills %v, unspent %d; want every skill by ID", got, f.checkpoints[0].GetUnspentSkillPoints())
	}
	if s.Retryable(grpctest.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND)) || !s.Retryable(status.Error(codes.Unavailable, "down")) {
		t.Fatal("only a failure without a reason is retried")
	}
}

type loads struct {
	characterv1.UnimplementedCharacterServiceServer
	char   *characterv1.Character
	quests []*characterv1.Quest
	skills []*characterv1.SkillPoints
	req    *characterv1.LoadRequest
}

func (f *loads) Load(_ context.Context, req *characterv1.LoadRequest) (*characterv1.LoadResponse, error) {
	f.req = req
	if f.char == nil {
		return nil, grpctest.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND)
	}
	return &characterv1.LoadResponse{Character: f.char, Quests: f.quests, Skills: f.skills}, nil
}

func TestCharactersLoadsAsTheWorldJoinsThem(t *testing.T) {
	id, account := kernel.NewCharacterID(), kernel.NewAccountID()
	f := &loads{char: &characterv1.Character{Id: id.String(), Name: "Ayla", Class: "bowman", Level: 3, Exp: 40,
		Points: &characterv1.Stats{Dexterity: 6, Agility: 4}, UnspentPoints: 0, FreeResetUsed: true, Hp: 90, Mp: 30, MapId: "cave", X: 12.5, Y: 600, UnspentSkillPoints: 3},
		skills: []*characterv1.SkillPoints{{SkillId: "throw", Points: 7}, {SkillId: "second_wind", Points: 2}},
		quests: []*characterv1.Quest{
			{QuestId: "slime_cleanup", Status: characterv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{7}},
			{QuestId: "first_steps", Status: characterv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 2},
		}}
	srv := grpcx.NewServer(grpcx.ServerOptions{})
	characterv1.RegisterCharacterServiceServer(srv, f)
	c := clients.NewCharacters(characterv1.NewCharacterServiceClient(grpctest.Conn(t, srv)))
	got, err := c.Load(context.Background(), id, account)
	if err != nil {
		t.Fatal(err)
	}
	want := model.Character{ID: id, Account: account, Name: "Ayla", Class: kernel.Class("bowman"), Level: 3, Exp: 40,
		Points: model.Points{Spent: gamedata.Stats{DEX: 6, AGI: 4}, FreeResetUsed: true}, HP: 90, MP: 30, MapID: "cave", X: 12.5, Y: 600,
		Skills: model.Skills{Points: map[string]int{"throw": 7, "second_wind": 2}, Unspent: 3},
		Quests: model.Quests{"slime_cleanup": {Status: model.QuestActive, Progress: []int{7}}, "first_steps": {Status: model.QuestFinished, TimesFinished: 2}}}
	if !reflect.DeepEqual(got, want) || f.req.GetAccountId() != account.String() || f.req.GetCharacterId() != id.String() {
		t.Fatalf("got %+v from %+v", got, f.req)
	}
	f.quests = []*characterv1.Quest{{QuestId: "q", Status: characterv1.QuestStatus(9)}}
	if _, err := c.Load(context.Background(), id, account); err == nil {
		t.Fatal("a quest status this build doesn't know joined")
	}
	f.char = nil
	_, err = c.Load(context.Background(), id, account)
	if r, ok := grpcx.ReasonOf(err); !ok || r != grpcx.Key(characterv1.Reason_REASON_CHARACTER_NOT_FOUND) {
		t.Fatalf("not found: %v", err)
	}
}

// A save is retried only while the character service may yet take it; a bug's Internal is given up.
func TestSavesRetryOnlyTransientFailures(t *testing.T) {
	s := clients.NewSaver(nil)
	if !s.Retryable(status.Error(codes.Unavailable, "restarting")) || !s.Retryable(status.Error(codes.DeadlineExceeded, "slow")) {
		t.Fatal("a transient failure is not retried")
	}
	if s.Retryable(status.Error(codes.Internal, "check constraint")) || s.Retryable(grpctest.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND)) {
		t.Fatal("a failure no retry can pass is retried")
	}
	if !s.Refused(grpctest.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND)) || s.Refused(status.Error(codes.Internal, "check constraint")) {
		t.Fatal("only a failure with a reason is a refusal; an Internal is unexplained")
	}
}
