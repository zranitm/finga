package model

// QuestOfferState is where a quest an NPC gives stands for the player.
type QuestOfferState int

// The offer states.
const (
	OfferAvailable QuestOfferState = iota + 1
	OfferActive
	OfferFinished // a one-time quest done
	OfferUnmet    // its conditions aren't met yet
)

// QuestConditions are what the player lacks to accept a quest; a zero field is met.
type QuestConditions struct {
	Level  int
	Class  string
	Quests []string
}

// QuestOffer is a quest an NPC gives, as it stands for the player.
type QuestOffer struct {
	Quest string
	State QuestOfferState
	Unmet QuestConditions
}

// QuestTurnIn is an active quest an NPC takes back, and whether its kills, talks and level are done.
type QuestTurnIn struct {
	Quest string
	Ready bool
}

// NpcDialog is what an NPC offers the player now.
type NpcDialog struct {
	NPC   string
	Gives []QuestOffer
	Takes []QuestTurnIn
}

// QuestStatus is where the player stands with a quest of its log; 0 is out of it.
type QuestStatus int

// The quest statuses.
const (
	QuestActive QuestStatus = iota + 1
	QuestFinished
)

// QuestState is one quest of the player's log.
type QuestState struct {
	Quest         string
	Status        QuestStatus
	Progress      []int
	TimesFinished int
}

// QuestChange is what happened to a quest.
type QuestChange int

// The quest changes.
const (
	QuestProgressed QuestChange = iota + 1
	QuestAccepted
	QuestHandedIn
	QuestAbandoned
)

// QuestChanged is one quest of the log after a tick.
type QuestChanged struct {
	Change QuestChange
	Quest  QuestState
}

// QuestLog replaces the player's whole log, after a join the player didn't ask for.
type QuestLog struct {
	Quests []QuestState
}

func (NpcDialog) push()    {}
func (QuestChanged) push() {}
func (QuestLog) push()     {}
