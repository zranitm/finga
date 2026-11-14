// Package handler is the character service's gRPC server.
package handler

import (
	"context"

	"google.golang.org/protobuf/types/known/timestamppb"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/character"
)

// Service is the character module's facade, as the handler calls it.
type Service interface {
	Create(ctx context.Context, account kernel.AccountID, name string) (character.Character, error)
	List(ctx context.Context, account kernel.AccountID) ([]character.Character, error)
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (character.Loaded, error)
	SaveCheckpoint(ctx context.Context, cp character.Checkpoint) error
	SaveLevelUp(ctx context.Context, lu character.LevelUp) error
	SetSkillBar(ctx context.Context, id kernel.CharacterID, account kernel.AccountID, bar []string) error
}

// Server serves CharacterService over the module's facade.
type Server struct {
	characterv1.UnimplementedCharacterServiceServer
	svc Service
}

// New builds the server over the facade.
func New(svc Service) *Server { return &Server{svc: svc} }

// Create makes a character and grants its starter items.
func (s *Server) Create(ctx context.Context, req *characterv1.CreateRequest) (*characterv1.CreateResponse, error) {
	account, err := kernel.ParseAccountID(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	c, err := s.svc.Create(ctx, account, req.GetName())
	if err != nil {
		return nil, err
	}
	return &characterv1.CreateResponse{Character: view(c)}, nil
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
		out = append(out, view(c))
	}
	return &characterv1.ListResponse{Characters: out}, nil
}

// Load is one character of the account, with its maximum HP, and its quests.
func (s *Server) Load(ctx context.Context, req *characterv1.LoadRequest) (*characterv1.LoadResponse, error) {
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	account, err := kernel.ParseAccountID(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	l, err := s.svc.Load(ctx, id, account)
	if err != nil {
		return nil, err
	}
	quests := make([]*characterv1.Quest, 0, len(l.Quests))
	for _, q := range l.Quests {
		quests = append(quests, questView(q))
	}
	skills := make([]*characterv1.SkillPoints, 0, len(l.Skills))
	for _, sk := range l.Skills {
		skills = append(skills, &characterv1.SkillPoints{SkillId: sk.ID, Points: int32(sk.Points)}) //nolint:gosec // points are small
	}
	timers := make([]*characterv1.SkillTimer, 0, len(l.Timers))
	for _, t := range l.Timers {
		timers = append(timers, timerView(t))
	}
	return &characterv1.LoadResponse{Character: view(l.Character), Quests: quests, Skills: skills, Timers: timers}, nil
}

// SaveCheckpoint saves position, HP, energy and the whole progression.
func (s *Server) SaveCheckpoint(ctx context.Context, req *characterv1.SaveCheckpointRequest) (*characterv1.SaveCheckpointResponse, error) {
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	cp := character.Checkpoint{
		ID: id, Class: kernel.Class(req.GetClass()), MapID: req.GetMapId(), X: req.GetX(), Y: req.GetY(), HP: int(req.GetHp()), Energy: int(req.GetEnergy()),
		Level: int(req.GetLevel()), Exp: req.GetExp(), Points: statsOf(req.GetPoints()), UnspentPoints: int(req.GetUnspentPoints()), FreeResetUsed: req.GetFreeResetUsed(),
	}
	for _, q := range req.GetQuests() {
		cp.Quests = append(cp.Quests, questOf(q))
	}
	cp.UnspentSkillPoints = int(req.GetUnspentSkillPoints())
	for _, sk := range req.GetSkills() {
		cp.Skills = append(cp.Skills, character.SkillPoints{ID: sk.GetSkillId(), Points: int(sk.GetPoints())})
	}
	for _, t := range req.GetTimers() {
		cp.Timers = append(cp.Timers, timerOf(t))
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
	lu := character.LevelUp{ID: id, Level: int(req.GetLevel()), Exp: req.GetExp(), Points: statsOf(req.GetPoints()), HP: int(req.GetHp())}
	if err := s.svc.SaveLevelUp(ctx, lu); err != nil {
		return nil, err
	}
	return &characterv1.SaveLevelUpResponse{}, nil
}

// SetSkillBar stores which skill sits on each key, for one of the account's characters.
func (s *Server) SetSkillBar(ctx context.Context, req *characterv1.SetSkillBarRequest) (*characterv1.SetSkillBarResponse, error) {
	id, err := kernel.ParseCharacterID(req.GetCharacterId())
	if err != nil {
		return nil, err
	}
	account, err := kernel.ParseAccountID(req.GetAccountId())
	if err != nil {
		return nil, err
	}
	if err := s.svc.SetSkillBar(ctx, id, account, req.GetSlots()); err != nil {
		return nil, err
	}
	return &characterv1.SetSkillBarResponse{}, nil
}

func view(c character.Character) *characterv1.Character {
	return &characterv1.Character{
		Id: c.ID.String(), AccountId: c.AccountID.String(), Name: c.Name, Class: c.Class.String(),
		Level: int32(c.Level), Exp: c.Exp, Points: statsView(c.Points), UnspentPoints: int32(c.UnspentPoints), FreeResetUsed: c.FreeResetUsed, OwnStats: statsView(c.OwnStats),
		UnspentSkillPoints: int32(c.UnspentSkillPoints),                                  //nolint:gosec // points are small
		Hp:                 int32(c.HP), MaxHp: int32(c.MaxHP), Energy: uint32(c.Energy), //nolint:gosec // HP fits, and energy is 0 to the data's max
		MapId: c.MapID, X: c.X, Y: c.Y, UpdatedAt: timestamppb.New(c.UpdatedAt), SkillBar: c.SkillBar,
	}
}

var questStatuses = map[character.QuestStatus]characterv1.QuestStatus{
	character.QuestActive:   characterv1.QuestStatus_QUEST_STATUS_ACTIVE,
	character.QuestFinished: characterv1.QuestStatus_QUEST_STATUS_FINISHED,
}

func questView(q character.Quest) *characterv1.Quest {
	progress := make([]int32, len(q.Progress))
	for k, n := range q.Progress {
		progress[k] = int32(n) //nolint:gosec // counts are small
	}
	return &characterv1.Quest{QuestId: q.ID, Status: questStatuses[q.Status], Progress: progress, TimesFinished: int32(q.TimesFinished)} //nolint:gosec // counts are small
}

var timerKinds = map[character.TimerKind]characterv1.SkillTimerKind{
	character.CooldownTimer: characterv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN,
	character.BuffTimer:     characterv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF,
}

func timerView(t character.SkillTimer) *characterv1.SkillTimer {
	return &characterv1.SkillTimer{SkillId: t.Skill, Kind: timerKinds[t.Kind], Until: timestamppb.New(t.Until), Points: int32(t.Points)} //nolint:gosec // points are small
}

// timerOf leaves an unknown kind empty, which the module refuses.
func timerOf(t *characterv1.SkillTimer) character.SkillTimer {
	out := character.SkillTimer{Skill: t.GetSkillId(), Until: t.GetUntil().AsTime(), Points: int(t.GetPoints())}
	for kind, wire := range timerKinds {
		if wire == t.GetKind() {
			out.Kind = kind
		}
	}
	return out
}

// questOf leaves an unknown status empty, which the module refuses.
func questOf(q *characterv1.Quest) character.Quest {
	out := character.Quest{ID: q.GetQuestId(), TimesFinished: int(q.GetTimesFinished())}
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
