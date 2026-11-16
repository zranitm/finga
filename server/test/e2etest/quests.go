package e2etest

import (
	"slices"
	"testing"
	"time"

	clientv1 "finga/api/gen/client/v1"
)

// QuestChanged waits for the quest's QuestChanged that satisfies ok.
func (l *Looter) QuestChanged(t *testing.T, quest string, d time.Duration, ok func(*clientv1.QuestChanged) bool) *clientv1.QuestChanged {
	t.Helper()
	return Take(t, l, "QuestChanged "+quest, &l.Quests, d, func(q *clientv1.QuestChanged) bool { return q.GetQuest().GetQuestId() == quest && ok(q) })
}

// HasQuest reports whether a QuestChanged of the quest that satisfies ok has arrived, leaving it recorded.
func (l *Looter) HasQuest(quest string, ok func(*clientv1.QuestChanged) bool) bool {
	return slices.ContainsFunc(l.Quests, func(q *clientv1.QuestChanged) bool { return q.GetQuest().GetQuestId() == quest && ok(q) })
}

// Talk opens the guide's dialog and returns it.
func (l *Looter) Talk(t *testing.T, req string) *clientv1.NpcDialog {
	t.Helper()
	if r := l.Command(t, req, &clientv1.Talk{NpcId: "town_guide"}); !r.GetOk() {
		t.Fatalf("talk: %+v", r)
	}
	return Take(t, l, "NpcDialog", &l.Dialogs, 5*time.Second, nil)
}
