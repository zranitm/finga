package main

import (
	"maps"
	"math"
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/pkg/wsconn"
)

func TestKillsAreTheBotsOwnKillingBlows(t *testing.T) {
	type event struct {
		tick    uint64
		removed []uint32         // a Delta's removed_mobs
		blow    *clientv1.Damage // else a Damage
	}
	mine := func(mob string) *clientv1.Damage { return &clientv1.Damage{Source: "p:c-bot", Target: mob, Amount: 9} }
	other := func(mob string) *clientv1.Damage {
		return &clientv1.Damage{Source: "p:c-other", Target: mob, Amount: 9}
	}
	next := event{tick: 99} // a later tick settles the one before
	cases := []struct {
		name   string
		events []event
		want   int
	}{
		{"its blow and the mob's removal in one tick, the Delta first as the world sends them", []event{{tick: 7, removed: []uint32{3}}, {tick: 7, blow: mine("m:3")}, next}, 1},
		{"the same, the Damage first", []event{{tick: 7, blow: mine("m:3")}, {tick: 7, removed: []uint32{3}}, next}, 1},
		{"its blow, then a party member's killing blow, in one tick", []event{{tick: 7, removed: []uint32{3}}, {tick: 7, blow: mine("m:3")}, {tick: 7, blow: other("m:3")}, next}, 0},
		{"a party member's blow, then its killing blow, in one tick", []event{{tick: 7, removed: []uint32{3}}, {tick: 7, blow: other("m:3")}, {tick: 7, blow: mine("m:3")}, next}, 1},
		{"its miss after a party member's killing blow", []event{{tick: 7, removed: []uint32{3}}, {tick: 7, blow: other("m:3")}, {tick: 7, blow: &clientv1.Damage{Source: "p:c-bot", Target: "m:3", Miss: true}}, next}, 0},
		{"its blow a tick before another's killing blow", []event{{tick: 6, blow: mine("m:3")}, {tick: 7, removed: []uint32{3}}, {tick: 7, blow: other("m:3")}, next}, 0},
		{"a mob hitting the bot as it dies", []event{{tick: 7, removed: []uint32{3}}, {tick: 7, blow: &clientv1.Damage{Source: "m:3", Target: "p:c-bot", Amount: 4}}, next}, 0},
		{"two mobs in one tick, one its own, and one the next", []event{{tick: 7, removed: []uint32{3, 4}}, {tick: 7, blow: other("m:3")}, {tick: 7, blow: mine("m:4")}, {tick: 8, removed: []uint32{5}}, {tick: 8, blow: mine("m:5")}, next}, 2},
		{"a tick not yet over", []event{{tick: 7, removed: []uint32{3}}, {tick: 7, blow: mine("m:3")}}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k, got := newKills(), 0
			for _, e := range c.events {
				if e.blow != nil {
					got += k.blow(e.tick, e.blow, "c-bot")
				} else {
					got += k.removedMobs(e.tick, e.removed, "c-bot")
				}
			}
			if got != c.want {
				t.Fatalf("kills %d, want %d", got, c.want)
			}
		})
	}
}

func TestTheReportPrintsBucketBoundsAsTheyAre(t *testing.T) {
	ticks := newHist()
	maps.Copy(ticks.buckets, map[float64]float64{0.0005: 90, 0.001: 99, 0.0025: 100, math.Inf(1): 100})
	ticks.count = 100
	if got, want := percentile(ticks, 0.5), "0.28 ms (bucket ≤ 0.5 ms)"; got != want {
		t.Errorf("p50 %q, want %q", got, want)
	}
	if got, want := percentile(ticks, 0.99), "1.00 ms (bucket ≤ 1 ms)"; got != want {
		t.Errorf("p99 %q, want %q", got, want)
	}

	batches := newHist()
	maps.Copy(batches.buckets, map[float64]float64{1: 649, 2: 649, 32: 649, math.Inf(1): 649})
	batches.count = 649
	if got, want := drops(batches.quantile(0.5)), "≤ 1"; got != want {
		t.Errorf("drops p50 %q, want %q: a call carries at least one drop", got, want)
	}
	batches.buckets[math.Inf(1)] = 650
	batches.count = 650
	if got, want := drops(batches.quantile(1)), "> 32"; got != want {
		t.Errorf("drops max %q, want %q", got, want)
	}
}

func TestTheBotHandsInWhatIsReadyAndTakesWhatIsOpen(t *testing.T) {
	data := gamedatatest.Full(t)
	gels := func(n int32) map[string]*clientv1.ItemView {
		return map[string]*clientv1.ItemView{"etc/0": {Template: "slime_gel", Quantity: n - 1}, "etc/3": {Template: "slime_gel", Quantity: 1}}
	}
	dialog := &clientv1.NpcDialog{
		NpcId: "town_guide",
		Gives: []*clientv1.QuestOffer{
			{QuestId: "slime_cleanup", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_FINISHED},
			{QuestId: "sticky_samples", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_ACTIVE},
			{QuestId: "meet_the_trainers", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_AVAILABLE},
			{QuestId: "ready_for_the_wilds", State: clientv1.QuestOfferState_QUEST_OFFER_STATE_CONDITIONS_UNMET},
		},
		Takes: []*clientv1.QuestTurnIn{{QuestId: "sticky_samples", Ready: true}},
	}
	accept := &clientv1.AcceptQuest{NpcId: "town_guide", QuestId: "meet_the_trainers"}
	handIn := &clientv1.HandInQuest{NpcId: "town_guide", QuestId: "sticky_samples"}
	for _, c := range []struct {
		name  string
		items map[string]*clientv1.ItemView
		want  []proto.Message
	}{
		{"the five gels held, across two stacks", gels(5), []proto.Message{handIn, accept}},
		{"a gel short", gels(4), []proto.Message{accept}},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := &bot{data: data, items: c.items}
			got := b.answer(dialog)
			if !slices.EqualFunc(got, c.want, proto.Equal) {
				t.Fatalf("answers %v, want %v", got, c.want)
			}
		})
	}
	if got := townNPCs(data, data.StartTown()); len(got) != 4 || got[0].ID != "town_guide" || got[1].ID != "warrior_trainer" || got[2].ID != "magician_trainer" || got[3].ID != "bowman_trainer" {
		t.Fatalf("a town visit talks to %v; want each next the nearest, from where players appear", got)
	}
}

// A bot keeps its own pouches from the frames, takes all in reach, and leaves a pouch whose take was refused.
func TestABotTakesItsPouchesInReachAndLeavesOnesRefused(t *testing.T) {
	b := &bot{data: gamedatatest.Full(t), id: "c-bot", x: 10, z: 10, kills: newKills()}
	pouch := func(id uint32, x float64, items ...uint32) *clientv1.PouchView {
		p := &clientv1.PouchView{Id: id, X: x, Z: 10}
		for _, it := range items {
			p.Items = append(p.Items, &clientv1.PouchItem{Item: it, Template: "slime_gel", Quantity: 1})
		}
		return p
	}
	b.observe(wsconn.Message{Data: &clientv1.Snapshot{Pouches: []*clientv1.PouchView{pouch(1, 10.5, 1, 2)}}})
	b.observe(wsconn.Message{Data: &clientv1.Delta{Pouches: []*clientv1.PouchView{pouch(2, 10, 1), pouch(3, 14, 1), pouch(4, 9.5, 1)}, PouchesGone: []uint32{4}}})
	want := &clientv1.TakeLoot{Picks: []*clientv1.LootPick{{Pouch: 1, Item: 1}, {Pouch: 1, Item: 2}, {Pouch: 2, Item: 1}}}
	if got := b.takeInReach(); !proto.Equal(got, want) {
		t.Fatalf("took %v, want the two pouches in reach, not the far one or the gone one", got)
	}
	b.taking = true
	b.observe(wsconn.Message{ReqID: takeReq, Data: &clientv1.Response{Ok: true, Picks: []*clientv1.PickAnswer{{Pick: &clientv1.LootPick{Pouch: 1, Item: 1}}, {Pick: &clientv1.LootPick{Pouch: 2, Item: 1}, Code: 220007}}}})
	b.observe(wsconn.Message{Data: &clientv1.Loot{Kind: clientv1.LootKind_LOOT_KIND_TAKEN, OwnerId: "c-bot", Items: []*clientv1.LootItem{{Template: "slime_gel", Quantity: 1}, {Template: "red_potion", Quantity: 1}}}})
	b.observe(wsconn.Message{Data: &clientv1.Loot{Kind: clientv1.LootKind_LOOT_KIND_TAKEN, OwnerId: "c-mate", Items: []*clientv1.LootItem{{Template: "slime_gel", Quantity: 1}}}})
	if b.taking || !b.leftPouches[2] || b.leftPouches[1] || b.snapshot().items != 2 || b.snapshot().refused != 1 {
		t.Fatalf("taking %v, left %v, %d items, %d refused; want the answer heard, the pouch with no room left, its own two items counted", b.taking, b.leftPouches, b.snapshot().items, b.snapshot().refused)
	}
	if got := b.takeInReach(); !proto.Equal(got, &clientv1.TakeLoot{Picks: []*clientv1.LootPick{{Pouch: 1, Item: 1}, {Pouch: 1, Item: 2}}}) {
		t.Fatalf("took %v, want only the pouch not left", got)
	}
}

// A party invite reaches only a character on the inviter's map, so the leading bot invites a bot there, not one elsewhere.
func TestABotInvitesABotOnItsOwnMap(t *testing.T) {
	lead, away, near, partied := &bot{id: "a", mapID: "willowmere"}, &bot{id: "b"}, &bot{id: "c"}, &bot{id: "d"}
	away.onMap.Store(new("greyfang_cave"))
	near.onMap.Store(new("willowmere"))
	partied.onMap.Store(new("willowmere"))
	partied.inParty.Store(true)
	if got := lead.invitee([]*bot{lead, partied, away, near}); got != near {
		t.Fatalf("invites %+v; want the bot in Willowmere with no party", got)
	}
	if got := lead.invitee([]*bot{lead, partied, away}); got != nil {
		t.Fatalf("invites %+v; want none, the only free bot being in the cave", got)
	}
}

// C18: a bot fights with its main skill from that skill's own key, naming it as the chain's root, and only when its class lets it stand there: a Beginner bound for a promotion asking Lunge, which the Beginner's chains make a follow-up, attacks instead.
func TestABotUsesOnlyAMainSkillItsClassCanKey(t *testing.T) {
	data := gamedatatest.Full(t)
	for _, c := range []struct {
		skill string
		used  bool
	}{{"spark", true}, {"lunge", false}} {
		future := class(t, data, "magician")
		future.Promotion.Skills = map[string]int{c.skill: 10}
		b := &bot{data: data, class: data.StartClass().ID, future: future, energy: 10, invested: map[string]int32{c.skill: 10}}
		if got := b.useSkill(0); got != c.used {
			t.Fatalf("%s: used %v, want %v", c.skill, got, c.used)
		}
		if !c.used {
			continue
		}
		if use, ok := b.actions[0].Data.(*clientv1.UseSkill); len(b.actions) != 1 || !ok || use.GetSkill() != c.skill || use.GetChainRoot() != c.skill {
			t.Fatalf("sent %+v, want one UseSkill of %s from its own key", b.actions, c.skill)
		}
	}
}
