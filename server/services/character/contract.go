package character

import (
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/character/internal/model"
)

// Character is a character as its callers see it, with its own stats and maximums.
type Character struct {
	ID                 kernel.CharacterID
	AccountID          kernel.AccountID
	Name               string
	Class              kernel.Class
	Level              int
	Exp                int64          // within the current level
	Points             gamedata.Stats // spent in each stat
	UnspentPoints      int
	FreeResetUsed      bool
	OwnStats           gamedata.Stats // the starting stats plus the points, without equipment
	UnspentSkillPoints int
	HP, MaxHP          int
	Energy             int
	MapID              string
	X, Y               float64 // feet centre, map pixels
	UpdatedAt          time.Time
	SkillBar           []string // the skill on each key, "" for none
}

// Loaded is a character as the world joins it: with its quests, skills and running skill timers.
type Loaded struct {
	Character Character
	Quests    []Quest
	Skills    []SkillPoints
	Timers    []SkillTimer
}

// SkillPoints are the points invested in one skill.
type SkillPoints struct {
	ID     string
	Points int
}

// QuestStatus is where a character stands with a quest.
type QuestStatus string

// The quest statuses; any other is refused.
const (
	QuestActive   QuestStatus = "active"
	QuestFinished QuestStatus = "finished"
)

// Quest is a character's state of one quest: active with a count per objective, or finished; TimesFinished counts every completion.
type Quest struct {
	ID            string
	Status        QuestStatus
	Progress      []int // empty when finished
	TimesFinished int
}

// TimerKind is what a skill timer times.
type TimerKind string

// The kinds of skill timer; any other is refused.
const (
	CooldownTimer TimerKind = "cooldown"
	BuffTimer     TimerKind = "buff"
)

// SkillTimer is when one of a skill's cooldown or buff ends; a buff keeps the points it was used with.
type SkillTimer struct {
	Skill  string
	Kind   TimerKind
	Until  time.Time
	Points int
}

// Checkpoint is the non-economic state saved periodically, on map change and on logout.
type Checkpoint struct {
	ID                 kernel.CharacterID
	Class              kernel.Class // a promotion changes it
	MapID              string
	X, Y               float64
	HP                 int
	Energy             int
	Level              int
	Exp                int64
	Points             gamedata.Stats
	UnspentPoints      int
	FreeResetUsed      bool
	Quests             []Quest // every quest the character has; a stored one not listed goes
	UnspentSkillPoints int
	Skills             []SkillPoints // every skill with points; a stored one not listed keeps its own
	Timers             []SkillTimer  // every cooldown and buff still running; a stored one not listed goes
}

// LevelUp is saved right away when a character gains a level.
type LevelUp struct {
	ID     kernel.CharacterID
	Level  int
	Exp    int64
	Points gamedata.Stats
	HP     int
}

func questOf(q model.Quest) Quest {
	return Quest{ID: q.ID, Status: QuestStatus(q.Status), Progress: q.Progress, TimesFinished: q.TimesFinished}
}

func (q Quest) model() model.Quest {
	return model.Quest{ID: q.ID, Status: model.QuestStatus(q.Status), Progress: q.Progress, TimesFinished: q.TimesFinished}
}

func timerOf(t model.SkillTimer) SkillTimer {
	return SkillTimer{Skill: t.Skill, Kind: TimerKind(t.Kind), Until: t.Until, Points: t.Points}
}

func (t SkillTimer) model() model.SkillTimer {
	return model.SkillTimer{Skill: t.Skill, Kind: model.TimerKind(t.Kind), Until: t.Until, Points: t.Points}
}

func (cp Checkpoint) model() model.Checkpoint {
	out := model.Checkpoint{
		ID: cp.ID, Class: cp.Class, MapID: cp.MapID, X: cp.X, Y: cp.Y, HP: cp.HP, Energy: cp.Energy, Level: cp.Level, Exp: cp.Exp,
		Points: cp.Points, UnspentPoints: cp.UnspentPoints, FreeResetUsed: cp.FreeResetUsed, UnspentSkillPoints: cp.UnspentSkillPoints,
	}
	for _, q := range cp.Quests {
		out.Quests = append(out.Quests, q.model())
	}
	for _, sk := range cp.Skills {
		out.Skills = append(out.Skills, model.SkillPoints(sk))
	}
	for _, t := range cp.Timers {
		out.Timers = append(out.Timers, t.model())
	}
	return out
}
