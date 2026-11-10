package clients

import (
	"context"
	"fmt"
	"maps"
	"slices"

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

// SaveCheckpoint saves position, HP, MP, the whole progression and the whole quest log.
func (s Saver) SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error {
	_, err := s.c.SaveCheckpoint(ctx, &characterv1.SaveCheckpointRequest{
		CharacterId: cp.ID.String(), MapId: cp.MapID, X: cp.X, Y: cp.Y, Hp: int32(cp.HP), Mp: int32(cp.MP),
		Level: int32(cp.Level), Exp: cp.Exp, Points: statsOf(cp.Points.Spent), UnspentPoints: int32(cp.Points.Unspent), FreeResetUsed: cp.Points.FreeResetUsed,
		Quests: questsOf(cp.Quests),
	})
	return err
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

// Load is the account's character as last saved.
func (c Characters) Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	res, err := c.c.Load(ctx, &characterv1.LoadRequest{CharacterId: id.String(), AccountId: account.String()})
	if err != nil {
		return model.Character{}, err
	}
	ch := res.GetCharacter()
	class, err := kernel.ParseClass(ch.GetClass())
	if err != nil {
		return model.Character{}, err
	}
	log, err := logOf(res.GetQuests())
	if err != nil {
		return model.Character{}, err
	}
	pts := ch.GetPoints()
	return model.Character{
		ID: id, Account: account, Name: ch.GetName(), Class: class, Level: int(ch.GetLevel()), Exp: ch.GetExp(),
		Points: model.Points{
			Spent:   gamedata.Stats{STR: int(pts.GetStrength()), DEX: int(pts.GetDexterity()), INT: int(pts.GetIntelligence()), AGI: int(pts.GetAgility()), VIT: int(pts.GetVitality())},
			Unspent: int(ch.GetUnspentPoints()), FreeResetUsed: ch.GetFreeResetUsed(),
		},
		HP: int(ch.GetHp()), MP: int(ch.GetMp()), MapID: ch.GetMapId(), X: ch.GetX(), Y: ch.GetY(), Quests: log,
	}, nil
}
