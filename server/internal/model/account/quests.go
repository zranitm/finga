package account

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
