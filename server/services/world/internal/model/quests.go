package model

import (
	"maps"
	"slices"

	"finga/internal/kernel"
)

// QuestStatus is where a character stands with a quest.
type QuestStatus uint8

// The quest statuses.
const (
	QuestActive QuestStatus = iota + 1
	QuestFinished
)

// Quest is one quest of a character's log: active with a count per objective, or finished. TimesFinished counts every completion, so a repeatable quest active again keeps it.
type Quest struct {
	Status        QuestStatus
	Progress      []int // one count per objective; nil when finished
	TimesFinished int
}

// Quests is a character's quest log by quest ID. Only its map's tick touches it; anything read off the tick, such as a checkpoint, takes a Clone.
type Quests map[string]Quest

// Clone copies the log down to each quest's counts.
func (q Quests) Clone() Quests {
	out := maps.Clone(q)
	for id, quest := range out {
		quest.Progress = slices.Clone(quest.Progress)
		out[id] = quest
	}
	return out
}

// OfferState is where a quest an NPC gives stands for one character.
type OfferState uint8

// The offer states.
const (
	OfferAvailable OfferState = iota + 1
	OfferActive
	OfferFinished // a one-time quest done
	OfferUnmet    // its conditions aren't met yet
)

// Unmet is what a character lacks to accept a quest; a zero field is met.
type Unmet struct {
	Level  int          // the least level, when the character's is lower
	Class  kernel.Class // the class asked for, when the character is another
	Quests []string     // quests to finish first
}

// QuestOffer is a quest an NPC gives, as it stands for the character.
type QuestOffer struct {
	Quest string
	State OfferState
	Unmet Unmet
}

// QuestTurnIn is an active quest an NPC takes back: Ready once every kill, talk and level objective is done; the item service counts what it asks to collect.
type QuestTurnIn struct {
	Quest string
	Ready bool
}

// NpcDialog is what an NPC offers the character now, computed from its live state.
type NpcDialog struct {
	Tick  uint64
	NPC   string
	Gives []QuestOffer
	Takes []QuestTurnIn
}

func (NpcDialog) event() {}
