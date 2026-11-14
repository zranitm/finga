// Package promotions holds what a trainer's promotion asks of a character, read alike for its dialog and for the promotion itself.
package promotions

import (
	"maps"
	"slices"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/modules/world/internal/model"
)

// Character is what the rules read of a character: its class, level, own stats without gear, skill points and quest log.
type Character struct {
	Class  kernel.Class
	Level  int
	Own    gamedata.Stats
	Skills map[string]int // points invested in each
	Log    model.Quests
}

// Offer is the promotion npc offers c, when npc trains a class whose parent is c's: each condition, with what c has.
func Offer(data *gamedata.Data, npc gamedata.NPC, c Character) (model.PromotionOffer, bool) {
	class, ok := data.Class(npc.Trainer)
	if !ok || class.Parent == "" || class.Parent != c.Class {
		return model.PromotionOffer{}, false
	}
	p := class.Promotion
	o := model.PromotionOffer{Class: class.ID, Conditions: []model.Condition{{Kind: model.ConditionLevel, Need: p.Level, Have: c.Level}}}
	for _, st := range []struct {
		id         string
		need, have int
	}{{"str", p.Stats.STR, c.Own.STR}, {"dex", p.Stats.DEX, c.Own.DEX}, {"int", p.Stats.INT, c.Own.INT}, {"agi", p.Stats.AGI, c.Own.AGI}, {"vit", p.Stats.VIT, c.Own.VIT}} {
		if st.need > 0 {
			o.Conditions = append(o.Conditions, model.Condition{Kind: model.ConditionStat, ID: st.id, Need: st.need, Have: st.have})
		}
	}
	for _, id := range slices.Sorted(maps.Keys(p.Skills)) {
		o.Conditions = append(o.Conditions, model.Condition{Kind: model.ConditionSkill, ID: id, Need: p.Skills[id], Have: c.Skills[id]})
	}
	for _, id := range p.Quests {
		have := 0
		if c.Log[id].TimesFinished > 0 {
			have = 1
		}
		o.Conditions = append(o.Conditions, model.Condition{Kind: model.ConditionQuest, ID: id, Need: 1, Have: have})
	}
	return o, true
}
