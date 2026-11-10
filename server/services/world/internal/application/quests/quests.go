// Package quests holds the quest rules the world checks: what an NPC offers a character, and what it would take back.
package quests

import (
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/services/world/internal/model"
)

// Character is what the rules read of a character: its level, class and quest log.
type Character struct {
	Level int
	Class kernel.Class
	Log   model.Quests
}

// Dialog is what npc offers c now: each quest it gives with its state, and each of c's active quests it takes back.
func Dialog(data *gamedata.Data, npc gamedata.NPC, c Character) model.NpcDialog {
	d := model.NpcDialog{NPC: npc.ID}
	for _, id := range npc.Gives {
		if q, ok := data.Quest(id); ok {
			d.Gives = append(d.Gives, Offer(q, c))
		}
	}
	for _, id := range npc.Takes {
		q, ok := data.Quest(id)
		if entry, active := c.Log[id]; ok && active && entry.Status == model.QuestActive {
			d.Takes = append(d.Takes, model.QuestTurnIn{Quest: id, Ready: Ready(q, entry, c.Level)})
		}
	}
	return d
}

// Offer is how q stands for c: active, finished for good, unmet with what c lacks, or available.
func Offer(q gamedata.Quest, c Character) model.QuestOffer {
	entry, taken := c.Log[q.ID]
	switch {
	case taken && entry.Status == model.QuestActive:
		return model.QuestOffer{Quest: q.ID, State: model.OfferActive}
	case taken && !q.Repeatable:
		return model.QuestOffer{Quest: q.ID, State: model.OfferFinished}
	}
	if u := unmet(q, c); u.Level != 0 || u.Class != "" || len(u.Quests) > 0 {
		return model.QuestOffer{Quest: q.ID, State: model.OfferUnmet, Unmet: u}
	}
	return model.QuestOffer{Quest: q.ID, State: model.OfferAvailable}
}

// unmet is what c lacks of q's conditions.
func unmet(q gamedata.Quest, c Character) model.Unmet {
	var u model.Unmet
	if c.Level < q.Level {
		u.Level = q.Level
	}
	if q.Class != "" && c.Class != q.Class {
		u.Class = q.Class
	}
	for _, id := range q.Requires {
		if c.Log[id].TimesFinished == 0 {
			u.Quests = append(u.Quests, id)
		}
	}
	return u
}

// Ready reports whether every kill, talk and level objective of an active quest is done; collect objectives are the item service's to count.
func Ready(q gamedata.Quest, entry model.Quest, level int) bool {
	for k, o := range q.Objectives {
		switch o.Kind {
		case gamedata.Reach:
			if level < o.Count {
				return false
			}
		case gamedata.Kill, gamedata.TalkTo:
			if Count(entry, k) < o.Count {
				return false
			}
		case gamedata.Collect:
		}
	}
	return true
}

// Count is an active quest's count for its kth objective; one the log lacks, as after content grew, is 0.
func Count(entry model.Quest, k int) int {
	if k < len(entry.Progress) {
		return entry.Progress[k]
	}
	return 0
}
