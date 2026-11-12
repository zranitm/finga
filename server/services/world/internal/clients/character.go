package clients

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/world/internal/model"
)

// Saver adapts the character service to world.Saver.
type Saver struct {
	c characterv1.CharacterServiceClient
}

// NewSaver adapts the character client.
func NewSaver(c characterv1.CharacterServiceClient) Saver { return Saver{c: c} }

// SaveCheckpoint saves position, HP, MP, the whole progression, the whole quest log and the skills' timers.
func (s Saver) SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error {
	_, err := s.c.SaveCheckpoint(ctx, &characterv1.SaveCheckpointRequest{
		CharacterId: cp.ID.String(), Class: cp.Class.String(), MapId: cp.MapID, X: cp.X, Y: cp.Y, Hp: int32(cp.HP), Mp: int32(cp.MP),
		Level: int32(cp.Level), Exp: cp.Exp, Points: statsOf(cp.Points.Spent), UnspentPoints: int32(cp.Points.Unspent), FreeResetUsed: cp.Points.FreeResetUsed,
		Quests: questsOf(cp.Quests), UnspentSkillPoints: int32(cp.Skills.Unspent), Skills: skillsOf(cp.Skills), //nolint:gosec // points are small
		Timers: timersOf(cp.Timers),
	})
	return err
}

// timersOf is the cooldowns then the buffs, each by skill, so a save is the same for the same timers.
func timersOf(t model.SkillTimers) []*characterv1.SkillTimer {
	out := make([]*characterv1.SkillTimer, 0, len(t.Cooldowns)+len(t.Buffs))
	for _, s := range slices.Sorted(maps.Keys(t.Cooldowns)) {
		out = append(out, &characterv1.SkillTimer{SkillId: s, Kind: characterv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN, Until: timestamppb.New(t.Cooldowns[s])})
	}
	for _, s := range slices.Sorted(maps.Keys(t.Buffs)) {
		b := t.Buffs[s]
		out = append(out, &characterv1.SkillTimer{SkillId: s, Kind: characterv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF, Until: timestamppb.New(b.Until), Points: int32(b.Points)}) //nolint:gosec // points are small
	}
	return out
}

// timersFrom is the stored timers; one of a kind from a newer service is left out.
func timersFrom(ts []*characterv1.SkillTimer) model.SkillTimers {
	out := model.SkillTimers{Cooldowns: map[string]time.Time{}, Buffs: map[string]model.BuffTimer{}}
	for _, t := range ts {
		switch t.GetKind() {
		case characterv1.SkillTimerKind_SKILL_TIMER_KIND_COOLDOWN:
			out.Cooldowns[t.GetSkillId()] = t.GetUntil().AsTime()
		case characterv1.SkillTimerKind_SKILL_TIMER_KIND_BUFF:
			out.Buffs[t.GetSkillId()] = model.BuffTimer{Until: t.GetUntil().AsTime(), Points: int(t.GetPoints())}
		}
	}
	return out
}

var questStatuses = map[model.QuestStatus]characterv1.QuestStatus{
	model.QuestActive:   characterv1.QuestStatus_QUEST_STATUS_ACTIVE,
	model.QuestFinished: characterv1.QuestStatus_QUEST_STATUS_FINISHED,
}

// questsOf is the log by quest ID, so a save is the same for the same log.
func questsOf(log model.Quests) []*characterv1.Quest {
	out := make([]*characterv1.Quest, 0, len(log))
	for _, id := range slices.Sorted(maps.Keys(log)) {
		q := log[id]
		progress := make([]int32, len(q.Progress))
		for k, n := range q.Progress {
			progress[k] = int32(n) //nolint:gosec // counts are small
		}
		out = append(out, &characterv1.Quest{QuestId: id, Status: questStatuses[q.Status], Progress: progress, TimesFinished: int32(q.TimesFinished)}) //nolint:gosec // counts are small
	}
	return out
}

// logOf is the quest log as loaded; a status this build doesn't know fails the join.
func logOf(quests []*characterv1.Quest) (model.Quests, error) {
	log := make(model.Quests, len(quests))
	for _, q := range quests {
		var status model.QuestStatus
		for s, wire := range questStatuses {
			if wire == q.GetStatus() {
				status = s
			}
		}
		if status == 0 {
			return nil, fmt.Errorf("character: quest %s has status %v", q.GetQuestId(), q.GetStatus())
		}
		entry := model.Quest{Status: status, TimesFinished: int(q.GetTimesFinished())}
		for _, n := range q.GetProgress() {
			entry.Progress = append(entry.Progress, int(n))
		}
		log[q.GetQuestId()] = entry
	}
	return log, nil
}

func statsOf(st gamedata.Stats) *characterv1.Stats {
	return &characterv1.Stats{Strength: int32(st.STR), Dexterity: int32(st.DEX), Intelligence: int32(st.INT), Agility: int32(st.AGI), Vitality: int32(st.VIT)}
}

// Retryable is a failure the character service may yet get past, such as its database restarting.
func (Saver) Retryable(err error) bool { return grpcx.IsTransient(err) }

// Refused is a failure with a reason: a rule of the character service, which no retry passes.
func (Saver) Refused(err error) bool { return grpcx.IsRule(err) }

// Characters adapts the character service to world.Characters.
type Characters struct {
	c characterv1.CharacterServiceClient
}

// NewCharacters adapts the character client.
func NewCharacters(c characterv1.CharacterServiceClient) Characters { return Characters{c: c} }

// skillsOf lists the invested points by skill ID, so a checkpoint always reads the same.
func skillsOf(s model.Skills) []*characterv1.SkillPoints {
	out := make([]*characterv1.SkillPoints, 0, len(s.Points))
	for _, skill := range slices.Sorted(maps.Keys(s.Points)) {
		out = append(out, &characterv1.SkillPoints{SkillId: skill, Points: int32(s.Points[skill])}) //nolint:gosec // points are small
	}
	return out
}

// Load is the account's character as last saved.
func (c Characters) Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	res, err := c.c.Load(ctx, &characterv1.LoadRequest{CharacterId: id.String(), AccountId: account.String()})
	if err != nil {
		return model.Character{}, err
	}
	ch := res.GetCharacter()
	log, err := logOf(res.GetQuests())
	if err != nil {
		return model.Character{}, err
	}
	pts := ch.GetPoints()
	skills := model.Skills{Points: make(map[string]int, len(res.GetSkills())), Unspent: int(ch.GetUnspentSkillPoints())}
	for _, sk := range res.GetSkills() {
		skills.Points[sk.GetSkillId()] = int(sk.GetPoints())
	}
	return model.Character{
		ID: id, Account: account, Name: ch.GetName(), Class: kernel.Class(ch.GetClass()), Level: int(ch.GetLevel()), Exp: ch.GetExp(),
		Points: model.Points{
			Spent:   gamedata.Stats{STR: int(pts.GetStrength()), DEX: int(pts.GetDexterity()), INT: int(pts.GetIntelligence()), AGI: int(pts.GetAgility()), VIT: int(pts.GetVitality())},
			Unspent: int(ch.GetUnspentPoints()), FreeResetUsed: ch.GetFreeResetUsed(),
		},
		Skills: skills,
		HP:     int(ch.GetHp()), MP: int(ch.GetMp()), MapID: ch.GetMapId(), X: ch.GetX(), Y: ch.GetY(), Quests: log,
		Timers: timersFrom(res.GetTimers()),
	}, nil
}
