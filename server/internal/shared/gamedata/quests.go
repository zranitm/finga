package gamedata

import "finga/internal/kernel"

// ObjectiveKind is what an objective asks for.
type ObjectiveKind string

// The four kinds of objective.
const (
	Kill    ObjectiveKind = "kill"
	Collect ObjectiveKind = "collect"
	TalkTo  ObjectiveKind = "talk"
	Reach   ObjectiveKind = "level"
)

// Objective is one task of a quest: Count of a mob (Kill) or an item (Collect), an NPC to talk to (Count 1), or a level to reach (Count is the level).
type Objective struct {
	Kind   ObjectiveKind
	Target string // the mob, item or NPC; empty for a level
	Count  int
}

// Rewards are what handing a quest in gives.
type Rewards struct {
	Exp   int64
	Items []Grant
}

// Quest is a task an NPC gives and another (often the same) takes back.
type Quest struct {
	ID         string
	Name       string
	Giver      string
	TurnIn     string
	Level      int          // the least level to accept it
	Class      kernel.Class // empty: any class
	Requires   []string     // quests finished before
	Objectives []Objective
	Rewards    Rewards
	Repeatable bool
}

// Quests lists every quest in file order.
func (d *Data) Quests() []Quest { return d.p.Quests }

// MaxActiveQuests is the most quests a character has active at once.
func (d *Data) MaxActiveQuests() int { return d.p.MaxActiveQuests }

// Quest finds a quest.
func (d *Data) Quest(id string) (Quest, bool) { return find(d.p.Quests, d.questIdx, id) }
