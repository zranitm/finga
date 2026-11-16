package promotions_test

import (
	"reflect"
	"testing"

	"finga/internal/gamedata"
	"finga/modules/world/internal/application/promotions"
	"finga/modules/world/internal/model"
	"finga/modules/world/worldtest"
)

func TestOffer(t *testing.T) {
	data := worldtest.LoadData(t)
	trainer, _ := data.NPC("warrior_trainer")
	guide, _ := data.NPC("town_guide")
	ready := promotions.Character{
		Class: "beginner", Level: 10, Own: gamedata.Stats{STR: 30, DEX: 7, INT: 7, AGI: 7, VIT: 7},
		Skills: map[string]int{"strike": 10}, Log: model.Quests{"warriors_trial": {Status: model.QuestFinished, TimesFinished: 1}},
	}
	short := promotions.Character{Class: "beginner", Level: 9, Own: gamedata.Stats{STR: 29}, Skills: map[string]int{"strike": 3}, Log: model.Quests{"warriors_trial": {Status: model.QuestActive}}}
	t.Run("a Beginner meeting every condition, each listed with what they have", func(t *testing.T) {
		o, ok := promotions.Offer(data, trainer, ready)
		want := model.PromotionOffer{Class: "warrior", Conditions: []model.Condition{
			{Kind: model.ConditionLevel, Need: 10, Have: 10},
			{Kind: model.ConditionStat, ID: "str", Need: 30, Have: 30},
			{Kind: model.ConditionSkill, ID: "strike", Need: 10, Have: 10},
			{Kind: model.ConditionQuest, ID: "warriors_trial", Need: 1, Have: 1},
		}}
		if !ok || !reflect.DeepEqual(o, want) || !o.Met() {
			t.Fatalf("offer %+v, %v; want %+v, met", o, ok, want)
		}
	})
	t.Run("a Beginner short of each, an active trial not counting as finished", func(t *testing.T) {
		o, ok := promotions.Offer(data, trainer, short)
		var have []int
		for _, c := range o.Conditions {
			have = append(have, c.Have)
		}
		if !ok || o.Met() || !reflect.DeepEqual(have, []int{9, 29, 3, 0}) {
			t.Fatalf("offer %+v, %v; want each condition unmet", o, ok)
		}
	})
	t.Run("none from an NPC who trains nothing, or for a character already past the class's parent", func(t *testing.T) {
		warrior := ready
		warrior.Class = "warrior"
		thief := ready
		thief.Class = "thief"
		for _, c := range []struct {
			npc gamedata.NPC
			who promotions.Character
		}{{guide, ready}, {trainer, warrior}, {trainer, thief}} {
			if o, ok := promotions.Offer(data, c.npc, c.who); ok {
				t.Errorf("%s offers a %s %+v", c.npc.ID, c.who.Class, o)
			}
		}
	})
}
