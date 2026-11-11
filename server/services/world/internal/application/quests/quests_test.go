package quests_test

import (
	"reflect"
	"testing"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/world/internal/application/quests"
	"finga/services/world/internal/model"
	"finga/services/world/worldtest"
)

func TestOffer(t *testing.T) {
	trial := gamedata.Quest{ID: "trial", Level: 10, Class: kernel.Warrior, Requires: []string{"basics", "drill"}}
	daily := gamedata.Quest{ID: "daily", Repeatable: true}
	once := gamedata.Quest{ID: "once"}
	finished := model.Quest{Status: model.QuestFinished, TimesFinished: 1}
	tests := []struct {
		name string
		q    gamedata.Quest
		c    quests.Character
		want model.QuestOffer
	}{
		{"every condition met", trial, quests.Character{Level: 10, Class: kernel.Warrior, Log: model.Quests{"basics": finished, "drill": finished}}, model.QuestOffer{Quest: "trial", State: model.OfferAvailable}},
		{"each unmet condition listed", trial, quests.Character{Level: 9, Class: kernel.Bowman, Log: model.Quests{"drill": finished}},
			model.QuestOffer{Quest: "trial", State: model.OfferUnmet, Unmet: model.Unmet{Level: 10, Class: kernel.Warrior, Quests: []string{"basics"}}}},
		{"a required quest active again after a completion still counts", trial,
			quests.Character{Level: 12, Class: kernel.Warrior, Log: model.Quests{"basics": {Status: model.QuestActive, Progress: []int{0}, TimesFinished: 1}, "drill": finished}},
			model.QuestOffer{Quest: "trial", State: model.OfferAvailable}},
		{"active", once, quests.Character{Log: model.Quests{"once": {Status: model.QuestActive}}}, model.QuestOffer{Quest: "once", State: model.OfferActive}},
		{"a one-time quest finished", once, quests.Character{Log: model.Quests{"once": finished}}, model.QuestOffer{Quest: "once", State: model.OfferFinished}},
		{"a repeatable quest finished is offered again", daily, quests.Character{Log: model.Quests{"daily": finished}}, model.QuestOffer{Quest: "daily", State: model.OfferAvailable}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quests.Offer(tt.q, tt.c); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReadyLeavesCollectingToTheItemService(t *testing.T) {
	q := gamedata.Quest{Objectives: []gamedata.Objective{
		{Kind: gamedata.Kill, Target: "green_slime", Count: 3},
		{Kind: gamedata.Collect, Target: "slime_gel", Count: 5},
		{Kind: gamedata.TalkTo, Target: "town_guide", Count: 1},
		{Kind: gamedata.Reach, Count: 4},
	}}
	tests := []struct {
		name     string
		progress []int
		level    int
		want     bool
	}{
		{"kills, talk and level done, nothing collected", []int{3, 0, 1, 0}, 4, true},
		{"a kill short", []int{2, 0, 1, 0}, 4, false},
		{"not talked to", []int{3, 0, 0, 0}, 4, false},
		{"a level short", []int{3, 0, 1, 0}, 3, false},
		{"counts missing, as after the quest grew", []int{3}, 4, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quests.Ready(q, model.Quest{Status: model.QuestActive, Progress: tt.progress}, tt.level); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandsItemsWhenItCollectsOrGivesAny(t *testing.T) {
	kill := gamedata.Objective{Kind: gamedata.Kill, Target: "green_slime", Count: 3}
	collect := gamedata.Objective{Kind: gamedata.Collect, Target: "slime_gel", Count: 5}
	potions := gamedata.Rewards{Exp: 5, Items: []gamedata.Grant{{Item: "red_potion", Quantity: 3}}}
	tests := []struct {
		name string
		q    gamedata.Quest
		want bool
	}{
		{"kills for EXP alone", gamedata.Quest{Objectives: []gamedata.Objective{kill}, Rewards: gamedata.Rewards{Exp: 5}}, false},
		{"something to collect", gamedata.Quest{Objectives: []gamedata.Objective{kill, collect}, Rewards: gamedata.Rewards{Exp: 5}}, true},
		{"an item reward", gamedata.Quest{Objectives: []gamedata.Objective{kill}, Rewards: potions}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quests.HandsItems(tt.q); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDialogTakesBackOnlyActiveQuests(t *testing.T) {
	data := worldtest.LoadData(t)
	guide, _ := data.NPC("town_guide")
	log := model.Quests{"slime_cleanup": {Status: model.QuestFinished, TimesFinished: 1}, "sticky_samples": {Status: model.QuestActive, Progress: []int{0}}}
	got := quests.Dialog(data, guide, quests.Character{Level: 2, Class: kernel.Warrior, Log: log})
	want := model.NpcDialog{NPC: "town_guide",
		Gives: []model.QuestOffer{
			{Quest: "slime_cleanup", State: model.OfferFinished}, {Quest: "sticky_samples", State: model.OfferActive},
			{Quest: "meet_the_trainers", State: model.OfferAvailable}, {Quest: "ready_for_the_wilds", State: model.OfferAvailable},
		},
		Takes: []model.QuestTurnIn{{Quest: "sticky_samples", Ready: true}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
