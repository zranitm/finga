package world

import (
	"maps"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/modules/world/internal/model"
)

// Presence is the character a command is for and the gateway connection it comes from; a command from an older connection is ignored.
type Presence struct {
	Character kernel.CharacterID
	Conn      string
}

// View is the joining player's own character, as Join answers it.
type View struct {
	ID     kernel.CharacterID
	Name   string
	Class  kernel.Class
	Level  int
	Exp    int64
	Stats  gamedata.Stats // own: the class's starting stats plus the points
	HP     int
	MaxHP  int
	Energy int // as it joins; each frame's recipient entry carries it after
	MapID  string
	X, Y   float64
	Sheet  Sheet
	// JoinSeq rises with each join, across restarts too, so the character's later login has the higher one.
	JoinSeq uint64
	Quests  map[string]Quest // the log the player joined with, by quest
	Timers  SkillTimers      // the cooldowns and buffs the player joined with, still running
	// TickRateHz is the maps' ticks per second, which the client steps by.
	TickRateHz int
}

// Sheet is what the character window shows: the stats with the equipment's and skills' shares, the points, and the basic attack's length in ticks.
type Sheet struct {
	progression.Sheet
	Points            Points
	Skills            Skills
	AttackLengthTicks uint64
}

// Points are the stat points spent and unspent, and whether the free reset is used.
type Points struct {
	Spent         gamedata.Stats
	Unspent       int
	FreeResetUsed bool
}

// Skills are the skill points invested, by skill, and those unspent.
type Skills struct {
	Points  map[string]int // by skill; one with none isn't listed
	Unspent int
}

// SkillTimers are when each skill's cooldown and buff end, by skill.
type SkillTimers struct {
	Cooldowns map[string]time.Time
	Buffs     map[string]BuffTimer
}

// BuffTimer is when a buff ends, and the points it was used with, which give its bonus.
type BuffTimer struct {
	Until  time.Time
	Points int
}

// Quest is one quest of a character's log: active with a count per objective, or finished; TimesFinished counts every completion.
type Quest struct {
	Status        QuestStatus // zero for a quest that left the log
	Progress      []int       // one count per objective; nil when finished
	TimesFinished int
}

// QuestStatus is whether a quest is active or finished.
type QuestStatus uint8

// The quest statuses, as the model counts them.
const (
	QuestActive QuestStatus = iota + 1
	QuestFinished
)

func viewOf(v model.View) View {
	out := View{
		ID: v.ID, Name: v.Name, Class: v.Class, Level: v.Level, Exp: v.Exp, Stats: v.Stats,
		HP: v.HP, MaxHP: v.MaxHP, Energy: v.Energy, MapID: v.MapID, X: v.X, Y: v.Y,
		Sheet: sheetOf(v.Sheet), JoinSeq: v.JoinSeq, Quests: make(map[string]Quest, len(v.Quests)),
		Timers: SkillTimers{Cooldowns: maps.Clone(v.Timers.Cooldowns), Buffs: make(map[string]BuffTimer, len(v.Timers.Buffs))},
	}
	for id, q := range v.Quests {
		out.Quests[id] = questOf(q)
	}
	for skill, b := range v.Timers.Buffs {
		out.Timers.Buffs[skill] = BuffTimer(b)
	}
	return out
}

// sheetOf copies the skill points, which the event outlives on its way to the gateway.
func sheetOf(s model.Sheet) Sheet {
	return Sheet{Sheet: s.Sheet, Points: Points(s.Points), Skills: Skills{Points: maps.Clone(s.Skills.Points), Unspent: s.Skills.Unspent}, AttackLengthTicks: s.AttackLengthTicks}
}

// questOf copies the counts, which the map's tick goes on updating.
func questOf(q model.Quest) Quest {
	return Quest{Status: QuestStatus(q.Status), Progress: slices.Clone(q.Progress), TimesFinished: q.TimesFinished}
}
