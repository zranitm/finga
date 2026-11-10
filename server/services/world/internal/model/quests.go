package model

import (
	"maps"
	"slices"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
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

// QuestChange is what happened to a quest of the log, for the combat channel's line.
type QuestChange uint8

// The quest changes, least telling first: when one tick holds several, the player hears the last of them in this order.
const (
	QuestProgressed QuestChange = iota + 1
	QuestAccepted
	QuestHandedIn
	QuestAbandoned
)

// QuestChanged is one quest of the player's log after a tick: Quest.Status is 0 when it left the log.
type QuestChanged struct {
	Tick   uint64
	ID     string
	Change QuestChange
	Quest  Quest
}

func (QuestChanged) event() {}

// HandInCall takes what a quest asks to collect and gives its item rewards, under a key the world derives from the character, the quest and the completion.
type HandInCall struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
	Key       string
	Quest     string
	Take      []gamedata.Grant
	Give      []Reward
}

// Reward is one item line a hand-in gives; equipment comes one per line, with its roll.
type Reward struct {
	Template string
	Quantity int
	Stats    *progression.ItemStats
}
