package e2e_test

import (
	"cmp"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	itemv1 "finga/api/gen/item/v1"
)

// quickCleanup has the guide's first quest ask for two slimes and one gel, and the second for one gel.
func quickCleanup() []dataEdit {
	return []dataEdit{
		{"quests.yaml", "      - {kill: green_slime, count: 10}\n", "      - {kill: green_slime, count: 2}\n      - {collect: slime_gel, count: 1}\n"},
		{"quests.yaml", "      - {collect: slime_gel, count: 5}\n", "      - {collect: slime_gel, count: 1}\n"},
	}
}

// questChanged waits for the quest's QuestChanged that satisfies ok.
func (l *looter) questChanged(t *testing.T, quest string, d time.Duration, ok func(*clientv1.QuestChanged) bool) *clientv1.QuestChanged {
	t.Helper()
	return take(t, l, "QuestChanged "+quest, &l.quests, d, func(q *clientv1.QuestChanged) bool { return q.GetQuest().GetQuestId() == quest && ok(q) })
}

// hasQuest reports whether a QuestChanged of the quest that satisfies ok has arrived, leaving it recorded.
func (l *looter) hasQuest(quest string, ok func(*clientv1.QuestChanged) bool) bool {
	return slices.ContainsFunc(l.quests, func(q *clientv1.QuestChanged) bool { return q.GetQuest().GetQuestId() == quest && ok(q) })
}

// talk opens the guide's dialog and returns it.
func (l *looter) talk(t *testing.T, req string) *clientv1.NpcDialog {
	t.Helper()
	if r := l.command(t, req, &clientv1.Talk{NpcId: "town_guide"}); !r.GetOk() {
		t.Fatalf("talk: %+v", r)
	}
	return take(t, l, "NpcDialog", &l.dialogs, 5*time.Second, nil)
}

func TestQuests(t *testing.T) {
	s := newStackOver(t, gameData(t, append(quickCleanup(), slimesDrop(oneGel))...))
	quinnToken, quinnID := s.createCharacter(t, "quinn", "Quinn", "warrior")
	riaToken, riaID := s.createCharacter(t, "ria", "Ria", "warrior")
	// Both stand by the guide, at x 496.
	s.place(t, quinnID, "town", 480, 608, 144, 0)
	s.place(t, riaID, "town", 515, 608, 144, 0)
	quinn, ria := newLooter(s.join(t, quinnToken, quinnID)), newLooter(s.join(t, riaToken, riaID))

	t.Run("H1/the-guide-offers-its-quests-and-both-accept-the-first", func(t *testing.T) {
		want := &clientv1.NpcDialog{NpcId: "town_guide", Gives: []*clientv1.QuestOffer{
			{QuestId: "slime_cleanup", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE},
			{QuestId: "sticky_samples", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, Unmet: &clientv1.QuestConditions{Level: 2, Quests: []string{"slime_cleanup"}}},
			{QuestId: "meet_the_trainers", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, Unmet: &clientv1.QuestConditions{Level: 2, Quests: []string{"slime_cleanup"}}},
			{QuestId: "ready_for_the_wilds", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, Unmet: &clientv1.QuestConditions{Quests: []string{"slime_cleanup"}}},
		}}
		if d := quinn.talk(t, "t-1"); !proto.Equal(d, want) {
			t.Fatalf("dialog %v, want %v", d, want)
		}
		for _, l := range []*looter{quinn, ria} {
			if r := l.command(t, "a-1", &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "slime_cleanup"}); !r.GetOk() {
				t.Fatalf("%s accepts: %+v", l.id, r)
			}
			l.questChanged(t, "slime_cleanup", 5*time.Second, func(q *clientv1.QuestChanged) bool {
				return q.GetChange() == clientv1.QuestChange_QUEST_CHANGE_ACCEPTED && slices.Equal(q.GetQuest().GetProgress(), []int32{0, 0})
			})
		}
		if r := quinn.command(t, "a-2", &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "sticky_samples"}); r.GetOk() || r.GetCode() != "quest_conditions_unmet" {
			t.Fatalf("accepting the second early: %+v", r)
		}
		if r := quinn.command(t, "h-1", &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "slime_cleanup"}); r.GetOk() || r.GetCode() != "quest_not_ready" {
			t.Fatalf("handing in with nothing done: %+v", r)
		}
	})

	t.Run("H1/a-party-kill-counts-for-both-members", func(t *testing.T) {
		if r := quinn.command(t, "p-1", &clientv1.PartyInvite{CharacterId: ria.id}); !r.GetOk() {
			t.Fatalf("invite: %+v", r)
		}
		inv := take(t, ria, "PartyInvited", &ria.invites, 5*time.Second, nil)
		if r := ria.command(t, "p-2", &clientv1.PartyRespond{InviteId: inv.GetInviteId(), Accept: true}); !r.GetOk() {
			t.Fatalf("accept the invite: %+v", r)
		}
		quinn.partyState(t, 2)
		// Ria waits on the forest's town portal, out of the slimes' field; Quinn hunts in it.
		ria.hop(t, 1216, "forest", 64)
		if r := ria.command(t, "t-2", &clientv1.Talk{NpcId: "town_guide"}); r.GetOk() || r.GetCode() != "npc_out_of_reach" {
			t.Fatalf("talking to the guide from the forest: %+v", r)
		}
		quinn.hop(t, 1216, "forest", 64)
		if !quinn.walkTo(380, 10*time.Second) {
			t.Fatalf("Quinn could not reach the slimes (at %.0f)", quinn.x)
		}
		done := func(q *clientv1.QuestChanged) bool {
			return q.GetChange() == clientv1.QuestChange_QUEST_CHANGE_PROGRESSED && slices.Equal(q.GetQuest().GetProgress(), []int32{2, 0})
		}
		// Ria is read alongside, so her messages never back up.
		for deadline, next := time.Now().Add(90*time.Second), time.Now(); !quinn.hasQuest("slime_cleanup", done); {
			if time.Now().After(deadline) {
				t.Fatalf("Quinn killed no two slimes in 90 s (quest changes %v)", quinn.quests)
			}
			if !time.Now().Before(next) {
				if err := quinn.attack(); err != nil {
					t.Fatal(err)
				}
				next = time.Now().Add(850 * time.Millisecond)
			}
			quinn.pump(50*time.Millisecond, nil)
			ria.pump(50*time.Millisecond, nil)
		}
		quinn.questChanged(t, "slime_cleanup", time.Second, done)
		ria.questChanged(t, "slime_cleanup", 5*time.Second, done)
	})

	t.Run("H1/accept-kill-collect-and-hand-in", func(t *testing.T) {
		// Out of the party, every gel Quinn's kills drop is hers: she needs one for each quest.
		if r := ria.command(t, "p-3", &clientv1.PartyLeave{}); !r.GetOk() {
			t.Fatalf("leave the party: %+v", r)
		}
		quinn.partyState(t, 1)
		deadline := time.Now().Add(90 * time.Second)
		for {
			_, gels, _ := s.itemTotals(t, quinn.id, "slime_gel")
			if gels >= 2 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("Quinn holds %d gels after 90 s of hunting", gels)
			}
			quinn.farm(2 * time.Second)
			ria.pump(100*time.Millisecond, nil)
		}
		quinn.hop(t, 64, "town", 1216)
		if !quinn.walkTo(496, 15*time.Second) {
			t.Fatalf("Quinn could not reach the guide (at %.0f)", quinn.x)
		}
		before := s.inventoryTotals(t, quinn.id)
		quinn.exp = nil
		if r := quinn.command(t, "h-2", &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "slime_cleanup"}); !r.GetOk() {
			t.Fatalf("hand in: %+v", r)
		}
		quinn.questChanged(t, "slime_cleanup", 5*time.Second, func(q *clientv1.QuestChanged) bool {
			return q.GetChange() == clientv1.QuestChange_QUEST_CHANGE_HANDED_IN && q.GetQuest().GetStatus() == clientv1.QuestStatus_QUEST_STATUS_FINISHED && q.GetQuest().GetTimesFinished() == 1
		})
		take(t, quinn, "ExpGained", &quinn.exp, 5*time.Second, func(e *clientv1.ExpGained) bool { return e.GetAmount() == 30 })
		after := s.inventoryTotals(t, quinn.id)
		if after["slime_gel"] != before["slime_gel"]-1 || after["red_potion"] != before["red_potion"]+10 {
			t.Fatalf("held %v before the hand-in and %v after; want one gel taken and ten potions given", before, after)
		}
		if r := quinn.command(t, "h-3", &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "slime_cleanup"}); r.GetOk() || r.GetCode() != "quest_not_ready" {
			t.Fatalf("handing in again: %+v", r)
		}
	})

	t.Run("H1/a-hand-in-whose-rewards-dont-fit-changes-nothing", func(t *testing.T) {
		if r := quinn.command(t, "a-3", &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "sticky_samples"}); !r.GetOk() {
			t.Fatalf("accept the second, now level 2: %+v", r)
		}
		for range 24 {
			s.give(t, quinn.id, "leather_cap", &itemv1.ItemStats{PhysicalDefense: 3})
		}
		before := s.inventoryTotals(t, quinn.id)
		if r := quinn.command(t, "h-4", &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "sticky_samples"}); r.GetOk() || r.GetCode() != "inventory_full" {
			t.Fatalf("handing in with a full Equip tab: %+v", r)
		}
		if after := s.inventoryTotals(t, quinn.id); after["slime_gel"] != before["slime_gel"] || after["leather_cap"] != 24 {
			t.Fatalf("held %v before the refused hand-in and %v after; want nothing changed", before, after)
		}
		d := quinn.talk(t, "t-3")
		if len(d.GetTakes()) != 1 || d.GetTakes()[0].GetQuestId() != "sticky_samples" || !d.GetTakes()[0].GetReady() {
			t.Fatalf("dialog %v; want the second still active and ready", d)
		}
	})

	t.Run("H1/the-log-survives-a-new-login", func(t *testing.T) {
		_, w := s.welcome(t, "quinn", quinn.id)
		want := []*clientv1.QuestState{
			{QuestId: "slime_cleanup", Status: clientv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 1},
			{QuestId: "sticky_samples", Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{0}},
		}
		got := w.GetQuests()
		slices.SortFunc(got, func(a, b *clientv1.QuestState) int { return cmp.Compare(a.GetQuestId(), b.GetQuestId()) })
		if !slices.EqualFunc(got, want, func(a, b *clientv1.QuestState) bool { return proto.Equal(a, b) }) {
			t.Fatalf("welcome quests %v, want %v", got, want)
		}
		s.checkInvariants(t)
	})
}
