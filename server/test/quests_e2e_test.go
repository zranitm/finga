package e2e_test

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/application/item"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/pkg/runner/runnertest"
	"finga/pkg/uuidv7"
	"finga/test/e2etest"
)

// quickCleanup has the guide's first quest ask for two slimes and one gel, and the second for one gel.
func quickCleanup() []e2etest.DataEdit {
	return []e2etest.DataEdit{
		{File: "quests.yaml", Old: "      - {kill: green_slime, count: 10}\n", New: "      - {kill: green_slime, count: 2}\n      - {collect: slime_gel, count: 1}\n"},
		{File: "quests.yaml", Old: "      - {collect: slime_gel, count: 5}\n", New: "      - {collect: slime_gel, count: 1}\n"},
	}
}

// guideByTheSlimes moves the guide from Willowbrook to Sunmeadow, 14 m west of the slimes' camp, so a hunt and a hand-in are a few steps; the camp keeps clear of it by a mob's sight.
func guideByTheSlimes(t testing.TB) e2etest.DataEdit {
	sp := e2etest.Spots(t)
	return e2etest.DataEdit{File: "regions/willowmere.yaml", Old: fmt.Sprintf("{id: town_guide, at: [%v, %v], yaw: 90}", sp.ByTheGuide.X+0.5, sp.ByTheGuide.Z+0.5), New: fmt.Sprintf("{id: town_guide, at: [%v, %v], yaw: 180}", sp.FieldGuide.X, sp.FieldGuide.Z)}
}

func TestQuests(t *testing.T) {
	t.Parallel()
	s := newStackOver(t, e2etest.GameData(t, slices.Concat(quickCleanup(), e2etest.QuickSlimes(t), []e2etest.DataEdit{slimesDrop(oneGel), everyKill, guideByTheSlimes(t)})...))
	quinnToken, quinnID := s.CreateCharacter(t, "quinn", "Quinn", "bowman")
	riaToken, riaID := s.CreateCharacter(t, "ria", "Ria", "warrior")
	// Both stand by the guide, in talking reach.
	byTheFieldGuide := gamedata.Point{X: e2etest.Spots(t).FieldGuide.X - 0.5, Z: e2etest.Spots(t).FieldGuide.Z - 0.5}
	s.Place(t, quinnID, "willowmere", byTheFieldGuide, 144, 0)
	s.Place(t, riaID, "willowmere", gamedata.Point{X: byTheFieldGuide.X + 1, Z: byTheFieldGuide.Z}, 144, 0)
	quinn, ria := e2etest.NewLooter(s.Join(t, quinnToken, quinnID)), e2etest.NewLooter(s.Join(t, riaToken, riaID))

	t.Run("H1/the-guide-offers-its-quests-and-both-accept-the-first", func(t *testing.T) {
		want := &clientv1.NpcDialog{NpcId: "town_guide", Gives: []*clientv1.QuestOffer{
			{QuestId: "slime_cleanup", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE},
			{QuestId: "sticky_samples", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, Unmet: &clientv1.QuestConditions{Level: 2, Quests: []string{"slime_cleanup"}}},
			{QuestId: "meet_the_trainers", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, Unmet: &clientv1.QuestConditions{Level: 2, Quests: []string{"slime_cleanup"}}},
			{QuestId: "ready_for_the_wilds", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET, Unmet: &clientv1.QuestConditions{Quests: []string{"slime_cleanup"}}},
		}}
		if d := quinn.Talk(t, "t-1"); !proto.Equal(d, want) {
			t.Fatalf("dialog %v, want %v", d, want)
		}
		for _, l := range []*e2etest.Looter{quinn, ria} {
			if r := l.Command(t, "a-1", &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "slime_cleanup"}); !r.GetOk() {
				t.Fatalf("%s accepts: %+v", l.ID, r)
			}
			l.QuestChanged(t, "slime_cleanup", 5*time.Second, func(q *clientv1.QuestChanged) bool {
				return q.GetChange() == clientv1.QuestChange_QUEST_CHANGE_ACCEPTED && slices.Equal(q.GetQuest().GetProgress(), []int32{0, 0})
			})
		}
		if r := quinn.Command(t, "a-2", &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "sticky_samples"}); r.GetOk() || r.GetCode() != code("QUEST_CONDITIONS_UNMET") {
			t.Fatalf("accepting the second early: %+v", r)
		}
		if r := quinn.Command(t, "h-1", &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "slime_cleanup"}); r.GetOk() || r.GetCode() != code("QUEST_NOT_READY") {
			t.Fatalf("handing in with nothing done: %+v", r)
		}
	})

	t.Run("H1/a-party-kill-counts-for-both-members", func(t *testing.T) {
		if r := quinn.Command(t, "p-1", &clientv1.PartyInvite{CharacterId: ria.ID}); !r.GetOk() {
			t.Fatalf("invite: %+v", r)
		}
		inv := e2etest.Take(t, ria, "PartyInvited", &ria.Invites, 5*time.Second, nil)
		if r := ria.Command(t, "p-2", &clientv1.PartyRespond{InviteId: inv.GetInviteId(), Accept: true}); !r.GetOk() {
			t.Fatalf("accept the invite: %+v", r)
		}
		quinn.PartyState(t, 2)
		// Ria waits west of the guide, out of the slimes' field; Quinn hunts in it.
		if !ria.WalkTo(e2etest.Spots(t).ClearOfSlimes, 30*time.Second) {
			t.Fatalf("Ria could not reach her spot (at %.2f, %.2f)", ria.X, ria.Z)
		}
		if r := ria.Command(t, "t-2", &clientv1.Talk{NpcId: "town_guide"}); r.GetOk() || r.GetCode() != code("NPC_OUT_OF_REACH") {
			t.Fatalf("talking to the guide from the field's west: %+v", r)
		}
		if !quinn.WalkTo(e2etest.Spots(t).SlimeField, 30*time.Second) {
			t.Fatalf("Quinn could not reach the slimes (at %.2f, %.2f)", quinn.X, quinn.Z)
		}
		done := func(q *clientv1.QuestChanged) bool {
			return q.GetChange() == clientv1.QuestChange_QUEST_CHANGE_PROGRESSED && slices.Equal(q.GetQuest().GetProgress(), []int32{2, 0})
		}
		// Ria is read alongside, so her messages never back up.
		if !quinn.HuntUntil(30*time.Second, func() bool { return quinn.HasQuest("slime_cleanup", done) }, func() { ria.Pump(10*time.Millisecond, nil) }) {
			t.Fatalf("Quinn killed no two slimes in 30 s (quest changes %v)", quinn.Quests)
		}
		quinn.QuestChanged(t, "slime_cleanup", time.Second, done)
		ria.QuestChanged(t, "slime_cleanup", 5*time.Second, done)
	})

	t.Run("H1/accept-kill-collect-and-hand-in", func(t *testing.T) {
		// Out of the party, every gel Quinn's kills leave is hers: she needs one for each quest, and takes them as she hunts.
		if r := ria.Command(t, "p-3", &clientv1.PartyLeave{}); !r.GetOk() {
			t.Fatalf("leave the party: %+v", r)
		}
		quinn.PartyState(t, 1)
		for round := 0; ; round++ { // each a second of the world's ticks
			_, gels, _ := s.ItemTotals(t, quinn.ID, "slime_gel")
			if gels >= 2 {
				break
			}
			if round == 30 {
				t.Fatalf("Quinn holds %d gels after 30 s of hunting", gels)
			}
			quinn.Farm(time.Second)
			ria.Pump(100*time.Millisecond, nil)
		}
		if !quinn.Clear(15 * time.Second) { // the last kills' pouches are taken before the hand-in's counts
			t.Fatalf("Quinn's pouches still down after 15 s: %v", quinn.Pouches)
		}
		if !quinn.WalkTo(byTheFieldGuide, 30*time.Second) {
			t.Fatalf("Quinn could not reach the guide (at %.2f, %.2f)", quinn.X, quinn.Z)
		}
		before := s.InventoryTotals(t, quinn.ID)
		quinn.Exp = nil
		if r := quinn.Command(t, "h-2", &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "slime_cleanup"}); !r.GetOk() {
			t.Fatalf("hand in: %+v", r)
		}
		quinn.QuestChanged(t, "slime_cleanup", 5*time.Second, func(q *clientv1.QuestChanged) bool {
			return q.GetChange() == clientv1.QuestChange_QUEST_CHANGE_HANDED_IN && q.GetQuest().GetStatus() == clientv1.QuestStatus_QUEST_STATUS_FINISHED && q.GetQuest().GetTimesFinished() == 1
		})
		e2etest.Take(t, quinn, "ExpGained", &quinn.Exp, 5*time.Second, func(e *clientv1.ExpGained) bool { return e.GetAmount() == 30 })
		after := s.InventoryTotals(t, quinn.ID)
		if after["slime_gel"] != before["slime_gel"]-1 || after["red_potion"] != before["red_potion"]+10 {
			t.Fatalf("held %v before the hand-in and %v after; want one gel taken and ten potions given", before, after)
		}
		// Item's transactions are named by item's own calls, also under a world call: HandIn, not HandInQuest.
		_, page := runnertest.Get(t, s.Ops+"/metrics")
		for _, op := range []string{"AddMany", "HandIn"} {
			if !strings.Contains(page, `finga_db_tx_duration_seconds_count{module="item",operation="`+op+`"}`) {
				t.Errorf("no item transaction labelled %s", op)
			}
		}
		if strings.Contains(page, `operation="HandInQuest"`) {
			t.Error("an item transaction is labelled with the world's HandInQuest")
		}
		if r := quinn.Command(t, "h-3", &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "slime_cleanup"}); r.GetOk() || r.GetCode() != code("QUEST_NOT_READY") {
			t.Fatalf("handing in again: %+v", r)
		}
	})

	t.Run("H1/a-hand-in-whose-rewards-dont-fit-changes-nothing", func(t *testing.T) {
		if r := quinn.Command(t, "a-3", &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "sticky_samples"}); !r.GetOk() {
			t.Fatalf("accept the second, now level 2: %+v", r)
		}
		for range 24 {
			s.Give(t, quinn.ID, "leather_cap", &progression.ItemStats{PhysicalDefense: 3})
		}
		before := s.InventoryTotals(t, quinn.ID)
		if r := quinn.Command(t, "h-4", &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "sticky_samples"}); r.GetOk() || r.GetCode() != code("EQUIP_TAB_FULL") {
			t.Fatalf("handing in with a full Equip tab: %+v", r)
		}
		if after := s.InventoryTotals(t, quinn.ID); after["slime_gel"] != before["slime_gel"] || after["leather_cap"] != 24 {
			t.Fatalf("held %v before the refused hand-in and %v after; want nothing changed", before, after)
		}
		d := quinn.Talk(t, "t-3")
		if len(d.GetTakes()) != 1 || d.GetTakes()[0].GetQuestId() != "sticky_samples" || !d.GetTakes()[0].GetReady() {
			t.Fatalf("dialog %v; want the second still active and ready", d)
		}
	})

	t.Run("H1/the-log-survives-a-new-login", func(t *testing.T) {
		_, w := s.Welcome(t, "quinn", quinn.ID)
		want := []*clientv1.QuestState{
			{QuestId: "slime_cleanup", Status: clientv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 1},
			{QuestId: "sticky_samples", Status: clientv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: []int32{0}},
		}
		got := w.GetQuests()
		slices.SortFunc(got, func(a, b *clientv1.QuestState) int { return cmp.Compare(a.GetQuestId(), b.GetQuestId()) })
		if !slices.EqualFunc(got, want, func(a, b *clientv1.QuestState) bool { return proto.Equal(a, b) }) {
			t.Fatalf("welcome quests %v, want %v", got, want)
		}
		s.CheckInvariants(t)
	})

	t.Run("H1/a-hand-in-the-world-missed-finishes-at-the-next-login", func(t *testing.T) {
		ctx := context.Background()
		var cap string
		if err := s.DB.QueryRow(ctx, `SELECT id::text FROM items WHERE owner_id = $1 AND template_id = 'leather_cap' AND location = 'inventory' LIMIT 1`, quinn.ID).Scan(&cap); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Items.Discard(ctx, s.Actor(t, quinn.ID), item.DiscardRequest{Key: uuidv7.NewString(), ItemID: e2etest.MustID(t, kernel.ParseItemID, cap), Quantity: 1}); err != nil {
			t.Fatal(err)
		}
		before := s.InventoryTotals(t, quinn.ID)
		// The item module hands the second quest in, as for a world that never got the reply.
		missed := item.HandInRequest{Quest: "sticky_samples", Completion: 1,
			Take: []gamedata.Grant{{Item: "slime_gel", Quantity: 1}},
			Give: []item.Give{{TemplateID: "leather_cap", Quantity: 1, Stats: &progression.ItemStats{PhysicalDefense: 3}}}}
		if _, err := s.Items.HandIn(ctx, s.Actor(t, quinn.ID), missed); err != nil {
			t.Fatal(err)
		}
		p, _ := s.Welcome(t, "quinn", quinn.ID)
		back := e2etest.NewLooter(p, &clientv1.Snapshot{})
		back.QuestChanged(t, "sticky_samples", 5*time.Second, func(q *clientv1.QuestChanged) bool {
			return q.GetChange() == clientv1.QuestChange_QUEST_CHANGE_HANDED_IN && q.GetQuest().GetStatus() == clientv1.QuestStatus_QUEST_STATUS_FINISHED && q.GetQuest().GetTimesFinished() == 1
		})
		e2etest.Take(t, back, "ExpGained", &back.Exp, 5*time.Second, func(e *clientv1.ExpGained) bool { return e.GetAmount() == 40 })
		if r := back.Command(t, "h-5", &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "sticky_samples"}); r.GetOk() || r.GetCode() != code("QUEST_NOT_READY") {
			t.Fatalf("handing in again: %+v", r)
		}
		after := s.InventoryTotals(t, quinn.ID)
		if after["slime_gel"] != before["slime_gel"]-1 || after["leather_cap"] != before["leather_cap"]+1 {
			t.Fatalf("held %v before the hand-in and %v after; want one gel taken and one cap given, once", before, after)
		}
		_, w := s.Welcome(t, "quinn", quinn.ID)
		if !slices.ContainsFunc(w.GetQuests(), func(q *clientv1.QuestState) bool {
			return q.GetQuestId() == "sticky_samples" && q.GetStatus() == clientv1.QuestStatus_QUEST_STATUS_FINISHED && q.GetTimesFinished() == 1
		}) {
			t.Fatalf("welcome quests %v; want the second finished once", w.GetQuests())
		}
		s.CheckInvariants(t)
	})
}
