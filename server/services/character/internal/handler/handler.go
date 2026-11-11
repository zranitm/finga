// Package handler is the character service's gRPC server.
package handler

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/services/character/internal/application"
	"finga/services/character/internal/model"
)

// Service is what the handler needs from the character module.
type Service interface {
	Create(ctx context.Context, account kernel.AccountID, in application.CreateInput) (model.Character, error)
	List(ctx context.Context, account kernel.AccountID) ([]model.Character, error)
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error)
	Quests(ctx context.Context, id kernel.CharacterID) ([]model.Quest, error)
	Own(c model.Character) (gamedata.Stats, error)
	Derived(c model.Character) (progression.Derived, error)
	SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error
	SaveLevelUp(ctx context.Context, lu model.LevelUp) error
}

// Server serves CharacterService over the module.
type Server struct {
	characterv1.UnimplementedCharacterServiceServer
	svc Service
}

// New builds the server.
func New(svc Service) *Server { return &Server{svc: svc} }

// Create makes a character and grants its starter items.
func (s *Server) Create(ctx context.Context, req *characterv1.CreateRequest) (*characterv1.CreateResponse, error) {
	account, err := kernel.ParseAccountID(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	// The class passes unchecked; the module answers class_unavailable.
	c, err := s.svc.Create(ctx, account, application.CreateInput{Name: req.GetName(), Class: kernel.Class(req.GetClass())})
	if err != nil {
		return nil, err
	}
	return &characterv1.CreateResponse{Character: s.view(c)}, nil
}

// List is an account's characters.
func (s *Server) List(ctx context.Context, req *characterv1.ListRequest) (*characterv1.ListResponse, error) {
	account, err := kernel.ParseAccountID(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	cs, err := s.svc.List(ctx, account)
	if err != nil {
		return nil, err
	}
	out := make([]*characterv1.Character, 0, len(cs))
	for _, c := range cs {
		out = append(out, s.view(c))
	}
	return &characterv1.ListResponse{Characters: out}, nil
}

// Load is one character of the account, with its maximum HP and MP, and its quests.
func (s *Server) Load(ctx context.Context, req *characterv1.LoadRequest) (*characterv1.LoadResponse, error) {
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	account, err := kernel.ParseAccountID(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	c, err := s.svc.Load(ctx, id, account)
	if err != nil {
		return nil, err
	}
	if _, err := s.svc.Derived(c); err != nil {
		return nil, err
	}
	qs, err := s.svc.Quests(ctx, id)
	if err != nil {
		return nil, err
	}
	quests := make([]*characterv1.Quest, 0, len(qs))
	for _, q := range qs {
		quests = append(quests, questView(q))
	}
	return &characterv1.LoadResponse{Character: s.view(c), Quests: quests}, nil
}

// SaveCheckpoint saves position, HP, MP and the whole progression.
func (s *Server) SaveCheckpoint(ctx context.Context, req *characterv1.SaveCheckpointRequest) (*characterv1.SaveCheckpointResponse, error) {
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	cp := model.Checkpoint{
		ID: id, MapID: req.GetMapId(), X: req.GetX(), Y: req.GetY(), HP: int(req.GetHp()), MP: int(req.GetMp()),
		Level: int(req.GetLevel()), Exp: req.GetExp(), Points: statsOf(req.GetPoints()), UnspentPoints: int(req.GetUnspentPoints()), FreeResetUsed: req.GetFreeResetUsed(),
	}
	for _, q := range req.GetQuests() {
		cp.Quests = append(cp.Quests, questOf(q))
	}
	if err := s.svc.SaveCheckpoint(ctx, cp); err != nil {
		return nil, err
	}
	return &characterv1.SaveCheckpointResponse{}, nil
}

// SaveLevelUp saves a level-up.
func (s *Server) SaveLevelUp(ctx context.Context, req *characterv1.SaveLevelUpRequest) (*characterv1.SaveLevelUpResponse, error) {
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	lu := model.LevelUp{ID: id, Level: int(req.GetLevel()), Exp: req.GetExp(), Points: statsOf(req.GetPoints()), HP: int(req.GetHp()), MP: int(req.GetMp())}
	if err := s.svc.SaveLevelUp(ctx, lu); err != nil {
		return nil, err
	}
	return &characterv1.SaveLevelUpResponse{}, nil
}

// view leaves the own stats and maximums 0 when the class is unknown (Load refuses that case).
func (s *Server) view(c model.Character) *characterv1.Character {
	own, _ := s.svc.Own(c)
	d, _ := s.svc.Derived(c)
	return &characterv1.Character{
		Id: c.ID.String(), AccountId: c.AccountID.String(), Name: c.Name, Class: c.Class.String(),
		Level: int32(c.Level), Exp: c.Exp, Points: statsView(c.Points), UnspentPoints: int32(c.UnspentPoints), FreeResetUsed: c.FreeResetUsed, OwnStats: statsView(own),
		Hp: int32(c.HP), MaxHp: int32(d.MaxHP), Mp: int32(c.MP), MaxMp: int32(d.MaxMP),
		MapId: c.MapID, X: c.X, Y: c.Y, UpdatedAt: timestamppb.New(c.UpdatedAt),
	}
}

var questStatuses = map[model.QuestStatus]characterv1.QuestStatus{
	model.QuestActive:   characterv1.QuestStatus_QUEST_STATUS_ACTIVE,
	model.QuestFinished: characterv1.QuestStatus_QUEST_STATUS_FINISHED,
}

func questView(q model.Quest) *characterv1.Quest {
	progress := make([]int32, len(q.Progress))
	for k, n := range q.Progress {
		progress[k] = int32(n) //nolint:gosec // counts are small
	}
	return &characterv1.Quest{QuestId: q.ID, Status: questStatuses[q.Status], Progress: progress, TimesFinished: int32(q.TimesFinished)} //nolint:gosec // counts are small
}

// questOf leaves an unknown status empty, which the module refuses.
func questOf(q *characterv1.Quest) model.Quest {
	out := model.Quest{ID: q.GetQuestId(), TimesFinished: int(q.GetTimesFinished())}
	for status, wire := range questStatuses {
		if wire == q.GetStatus() {
			out.Status = status
		}
	}
	for _, n := range q.GetProgress() {
		out.Progress = append(out.Progress, int(n))
	}
	return out
}

func statsView(s gamedata.Stats) *characterv1.Stats {
	return &characterv1.Stats{Strength: int32(s.STR), Dexterity: int32(s.DEX), Intelligence: int32(s.INT), Agility: int32(s.AGI), Vitality: int32(s.VIT)}
}

func statsOf(s *characterv1.Stats) gamedata.Stats {
	return gamedata.Stats{STR: int(s.GetStrength()), DEX: int(s.GetDexterity()), INT: int(s.GetIntelligence()), AGI: int(s.GetAgility()), VIT: int(s.GetVitality())}
}
