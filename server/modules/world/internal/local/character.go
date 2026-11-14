package local

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"finga/internal/kernel"
	"finga/modules/character"
	"finga/modules/world/internal/model"
	"finga/pkg/transient"
)

// CharacterModule is the character module as the world calls it.
type CharacterModule interface {
	Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (character.Loaded, error)
	SaveCheckpoint(ctx context.Context, cp character.Checkpoint) error
}

// Saver adapts the character module to world.Saver.
type Saver struct{ c CharacterModule }

// NewSaver adapts the character module.
func NewSaver(c CharacterModule) Saver { return Saver{c: c} }

// SaveCheckpoint saves position, HP, energy, the whole progression, the whole quest log and the skills' timers.
func (s Saver) SaveCheckpoint(ctx context.Context, cp model.Checkpoint) error {
	return s.c.SaveCheckpoint(ctx, character.Checkpoint{
		ID: cp.ID, Class: cp.Class, MapID: cp.MapID, X: cp.X, Y: cp.Y, HP: cp.HP, Energy: cp.Energy, Level: cp.Level, Exp: cp.Exp,
		Points: cp.Points.Spent, UnspentPoints: cp.Points.Unspent, FreeResetUsed: cp.Points.FreeResetUsed,
		Quests: questsOf(cp.Quests), UnspentSkillPoints: cp.Skills.Unspent, Skills: skillsOf(cp.Skills), Timers: timersOf(cp.Timers),
	})
}

// Retryable is a failure the character module may yet get past, such as its database restarting.
func (Saver) Retryable(err error) bool { return transientErr(err) }

// Refused is a failure by a rule of the character module, which no retry passes.
func (Saver) Refused(err error) bool { return kernel.IsRule(err) }

// transientErr is a failure a later try may pass: a module unavailable, busy or stopping, or the call out of time or given up.
func transientErr(err error) bool {
	return errors.Is(err, transient.ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

var questStatuses = map[model.QuestStatus]character.QuestStatus{
	model.QuestActive:   character.QuestActive,
	model.QuestFinished: character.QuestFinished,
}

// questsOf is the log by quest ID, so a save is the same for the same log.
func questsOf(log model.Quests) []character.Quest {
	out := make([]character.Quest, 0, len(log))
	for _, id := range slices.Sorted(maps.Keys(log)) {
		q := log[id]
		out = append(out, character.Quest{ID: id, Status: questStatuses[q.Status], Progress: counts(q.Progress), TimesFinished: q.TimesFinished})
	}
	return out
}

// counts copies a quest's counts, none as nil, as they come through the wire.
func counts(in []int) []int { return append([]int(nil), in...) }

// skillsOf lists the invested points by skill ID, so a checkpoint always reads the same.
func skillsOf(s model.Skills) []character.SkillPoints {
	out := make([]character.SkillPoints, 0, len(s.Points))
	for _, skill := range slices.Sorted(maps.Keys(s.Points)) {
		out = append(out, character.SkillPoints{ID: skill, Points: s.Points[skill]})
	}
	return out
}

// timersOf is the cooldowns then the buffs, each by skill, so a save is the same for the same timers.
func timersOf(t model.SkillTimers) []character.SkillTimer {
	out := make([]character.SkillTimer, 0, len(t.Cooldowns)+len(t.Buffs))
	for _, s := range slices.Sorted(maps.Keys(t.Cooldowns)) {
		out = append(out, character.SkillTimer{Skill: s, Kind: character.CooldownTimer, Until: t.Cooldowns[s]})
	}
	for _, s := range slices.Sorted(maps.Keys(t.Buffs)) {
		b := t.Buffs[s]
		out = append(out, character.SkillTimer{Skill: s, Kind: character.BuffTimer, Until: b.Until, Points: b.Points})
	}
	return out
}

// Characters adapts the character module to world.Characters.
type Characters struct{ c CharacterModule }

// NewCharacters adapts the character module.
func NewCharacters(c CharacterModule) Characters { return Characters{c: c} }

// Load is the account's character as last saved.
func (l Characters) Load(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (model.Character, error) {
	loaded, err := l.c.Load(ctx, id, account)
	if err != nil {
		return model.Character{}, err
	}
	ch := loaded.Character
	log, err := logOf(loaded.Quests)
	if err != nil {
		return model.Character{}, err
	}
	skills := model.Skills{Points: make(map[string]int, len(loaded.Skills)), Unspent: ch.UnspentSkillPoints}
	for _, sk := range loaded.Skills {
		skills.Points[sk.ID] = sk.Points
	}
	return model.Character{
		ID: id, Account: account, Name: ch.Name, Class: ch.Class, Level: ch.Level, Exp: ch.Exp,
		Points: model.Points{Spent: ch.Points, Unspent: ch.UnspentPoints, FreeResetUsed: ch.FreeResetUsed},
		Skills: skills,
		HP:     ch.HP, Energy: ch.Energy, MapID: ch.MapID, X: ch.X, Y: ch.Y, Quests: log,
		Timers: timersFrom(loaded.Timers),
	}, nil
}

// logOf is the quest log as loaded; a status this build doesn't know fails the join.
func logOf(quests []character.Quest) (model.Quests, error) {
	log := make(model.Quests, len(quests))
	for _, q := range quests {
		var status model.QuestStatus
		for s, stored := range questStatuses {
			if stored == q.Status {
				status = s
			}
		}
		if status == 0 {
			return nil, fmt.Errorf("character: quest %s has status %q", q.ID, q.Status)
		}
		log[q.ID] = model.Quest{Status: status, TimesFinished: q.TimesFinished, Progress: counts(q.Progress)}
	}
	return log, nil
}

// timersFrom is the stored timers; one of a kind this build doesn't know is left out.
func timersFrom(ts []character.SkillTimer) model.SkillTimers {
	out := model.SkillTimers{Cooldowns: map[string]time.Time{}, Buffs: map[string]model.BuffTimer{}}
	for _, t := range ts {
		switch t.Kind {
		case character.CooldownTimer:
			out.Cooldowns[t.Skill] = t.Until
		case character.BuffTimer:
			out.Buffs[t.Skill] = model.BuffTimer{Until: t.Until, Points: t.Points}
		}
	}
	return out
}
