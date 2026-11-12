// Package model is the character service's model: the stored character, its saves and errors.
package model

import (
	"errors"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
)

// The service's errors.
var (
	ErrNotFound         = errors.New("character: not found")
	ErrNameTaken        = errors.New("character: name taken")
	ErrBadName          = errors.New("character: bad name")
	ErrClassUnavailable = errors.New("character: class not available")
	ErrSlotsFull        = errors.New("character: account has no free character slot")
	ErrBadSkill         = errors.New("character: a skill with no ID or one listed twice, unspent skill points below 0, or points below 1")
	ErrBadQuest         = errors.New("character: a quest with no ID or one listed twice, an unknown status, a negative count, or finished without a completion")
	ErrBadSkillBar      = errors.New("character: a skill bar without six keys, or with a key holding a skill that isn't an active one in the game data")
	ErrBadSkillTimer    = errors.New("character: a skill timer with no skill, an unknown kind, listed twice, or a buff without points")
)

// SkillBarKeys is how many keys the skill bar has: 1 to 6.
const SkillBarKeys = 6

// Character is a character as stored.
type Character struct {
	ID            kernel.CharacterID
	AccountID     kernel.AccountID
	Name          string
	Class         kernel.Class
	Level         int
	Exp           int64          // within the current level
	Points        gamedata.Stats // spent in each stat; the own stats are the class's starting stats plus these
	UnspentPoints int
	FreeResetUsed bool
	HP            int
	MP            int
	MapID         string
	X, Y          float64 // feet centre, map pixels
	UpdatedAt     time.Time

	UnspentSkillPoints int
	SkillBar           []string // the skill on each key, "" for none
}

// SkillPoints are the points invested in one skill.
type SkillPoints struct {
	ID     string
	Points int
}

// QuestStatus is where a character stands with a quest.
type QuestStatus string

// The quest statuses.
const (
	QuestActive   QuestStatus = "active"
	QuestFinished QuestStatus = "finished"
)

// Quest is a character's state of one quest: active with a count per objective, or finished. TimesFinished counts every completion, so a repeatable quest active again keeps it.
type Quest struct {
	ID            string
	Status        QuestStatus
	Progress      []int // empty when finished
	TimesFinished int
}

// Valid reports whether q is a quest row the table can hold.
func (q Quest) Valid() bool {
	if q.ID == "" || q.TimesFinished < 0 || slices.ContainsFunc(q.Progress, func(n int) bool { return n < 0 }) {
		return false
	}
	switch q.Status {
	case QuestActive:
		return true
	case QuestFinished:
		return q.TimesFinished >= 1 && len(q.Progress) == 0
	}
	return false
}

// Checkpoint is the non-economic state saved periodically, on map change and logout.
type Checkpoint struct {
	ID            kernel.CharacterID
	Class         kernel.Class // a promotion changes it
	MapID         string
	X, Y          float64
	HP            int
	MP            int
	Level         int
	Exp           int64
	Points        gamedata.Stats
	UnspentPoints int
	FreeResetUsed bool
	Quests        []Quest // every quest the character has; a stored one not listed goes

	UnspentSkillPoints int
	Skills             []SkillPoints // every skill with points; a stored one not listed keeps its own
	Timers             []SkillTimer  // every cooldown and buff still running; a stored one not listed goes
}

// TimerKind is what a skill timer times.
type TimerKind string

// The kinds of skill timer.
const (
	CooldownTimer TimerKind = "cooldown"
	BuffTimer     TimerKind = "buff"
)

// SkillTimer is when one of a skill's cooldown or buff ends; a buff keeps the points it was used with, which give its bonus.
type SkillTimer struct {
	Skill  string
	Kind   TimerKind
	Until  time.Time
	Points int
}

// Valid is a timer with a skill and a known kind, and a buff with points.
func (t SkillTimer) Valid() bool {
	return t.Skill != "" && (t.Kind == CooldownTimer && t.Points >= 0 || t.Kind == BuffTimer && t.Points >= 1)
}

// LevelUp is saved right away when a character gains a level.
type LevelUp struct {
	ID     kernel.CharacterID
	Level  int
	Exp    int64
	Points gamedata.Stats
	HP     int
	MP     int
}
