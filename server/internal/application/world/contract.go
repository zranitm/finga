package world

import (
	"maps"
	"slices"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
)

// SkillUse is a press of a skill: the skill, its key's root (the skill itself on its own key), the yaw it aims along, the lock (zero for none), and a moving skill's way.
type SkillUse struct {
	Skill, Root string
	Aim         float32
	Target      EntityRef
	Way         sim.Way
}

// Presence is the character a command is for and the gateway connection it comes from; a command from an older connection is ignored.
type Presence struct {
	Character kernel.CharacterID
	Conn      string
}

// Channels are a region's open channels and how full each is, against the players a channel takes before the next opens.
type Channels struct {
	Open []ChannelLoad
	Cap  int
}

// ChannelLoad is a channel's number and its players, those on their way included.
type ChannelLoad struct {
	Channel int
	Players int
}

// AreaBoss is a region's hunting area's field boss: up, or beaten and back in BackIn.
type AreaBoss struct {
	Area   string
	Up     bool
	BackIn time.Duration
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
	Sheet  Sheet
	// JoinSeq rises with each join, across restarts too, so the character's later login has the higher one.
	JoinSeq uint64
	Quests  map[string]Quest // the log the player joined with, by quest
	Timers  SkillTimers      // the cooldowns and buffs the player joined with, still running
	// Waypoints are the ids of those the character has found, in order.
	Waypoints []string
	// TickRateHz is the maps' ticks per second, which the client steps by.
	TickRateHz int
}

// Sheet is what the character window shows: the stats with the equipment's and skills' shares, the points, and the basic attack's length in ticks; and the buffs running.
type Sheet struct {
	progression.Sheet
	Points            Points
	Skills            Skills
	AttackLengthTicks uint64
	Buffs             []BuffView
}

// BuffView is a buff on the character: its skill, what's left of it, the points it was used with, and who used it, zero for the character's own.
type BuffView struct {
	Skill  string
	Left   time.Duration
	Points int
	From   kernel.CharacterID
}

// Points are the stat points spent and unspent, and whether the free reset is used.
type Points struct {
	Spent         gamedata.Stats
	Unspent       int
	FreeResetUsed bool
}

// Skills are the skill points invested, by skill, those unspent, and each skill's chosen trait.
type Skills struct {
	Points  map[string]int    // by skill; one with none isn't listed
	Traits  map[string]string // by skill; one on its default isn't listed
	Unspent int
}

// SkillTimers are when each skill's cooldown and buff end, by skill.
type SkillTimers struct {
	Cooldowns map[string]time.Time
	Buffs     map[string]BuffTimer
}

// BuffTimer is when a buff ends, the points and trait it was used with, which give its bonus, a shield's pool left, and who used it.
type BuffTimer struct {
	Until  time.Time
	Points int
	Trait  string
	Shield int
	From   kernel.CharacterID
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
		HP: v.HP, MaxHP: v.MaxHP, Energy: v.Energy, MapID: v.MapID,
		Sheet: sheetOf(v.Sheet), JoinSeq: v.JoinSeq, Quests: make(map[string]Quest, len(v.Quests)),
		Timers: timersOf(v.Timers), Waypoints: slices.Sorted(maps.Keys(v.Waypoints)),
	}
	for id, q := range v.Quests {
		out.Quests[id] = questOf(q)
	}
	return out
}

// timersOf copies the model's timers.
func timersOf(t model.SkillTimers) SkillTimers {
	out := SkillTimers{Cooldowns: maps.Clone(t.Cooldowns), Buffs: make(map[string]BuffTimer, len(t.Buffs))}
	for skill, b := range t.Buffs {
		out.Buffs[skill] = BuffTimer(b)
	}
	return out
}

// sheetOf copies the skill points, traits and buffs, which the event outlives on its way to the gateway.
func sheetOf(s model.Sheet) Sheet {
	out := Sheet{Sheet: s.Sheet, Points: Points(s.Points), Skills: Skills{Points: maps.Clone(s.Skills.Points), Traits: maps.Clone(s.Skills.Traits), Unspent: s.Skills.Unspent}, AttackLengthTicks: s.AttackLengthTicks}
	for _, b := range s.Buffs {
		out.Buffs = append(out.Buffs, BuffView(b))
	}
	return out
}

// questOf copies the counts, which the map's tick goes on updating.
func questOf(q model.Quest) Quest {
	return Quest{Status: QuestStatus(q.Status), Progress: slices.Clone(q.Progress), TimesFinished: q.TimesFinished}
}
