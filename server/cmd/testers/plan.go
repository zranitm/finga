package main

import (
	"fmt"

	characterv1 "finga/api/gen/character/v1"
	"finga/cmd/internal/spend"
	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
)

// stage is how far a test character is taken.
type stage int

const (
	fresh      stage = iota // level 1, as created
	promotable              // at its class's promotion level with every condition met, its trial done but not handed in
	capped                  // at the level cap, promoted, in the best gear it can wear, with potions
)

// tester is one test account and its one character, named for its class.
type tester struct {
	username string
	name     string
	// future is the first class it is, or is ready to become.
	future kernel.Class
	stage  stage
}

// testers are the accounts make testers keeps on a stack.
var testers = []tester{
	{username: "test_warrior", name: "TestWarrior", future: "warrior", stage: capped},
	{username: "test_thief", name: "TestThief", future: "thief", stage: capped},
	{username: "test_bowman", name: "TestBowman", future: "bowman", stage: capped},
	{username: "test_magician", name: "TestMagician", future: "magician", stage: capped},
	{username: "test_promote", name: "TestPromote", future: "warrior", stage: promotable},
	{username: "test_new", name: "TestNew", stage: fresh},
}

// piece is an item a seed gives: worn in slot, or in the bag when slot is empty.
type piece struct {
	template string
	slot     string
	quantity int
	roll     *progression.ItemStats
}

// seed is what a test character is given: its progression, written as the world saves it, then the points it spends and the items it wears, as a player would.
type seed struct {
	class  gamedata.Class
	level  int
	hp     int
	energy int
	quests []*characterv1.Quest
	// unspent and unspentSkills are the level's points, which stats and skills then spend.
	unspent, unspentSkills int
	stats                  gamedata.Stats
	skills                 map[string]int32
	gear, potions          []piece
}

// plan works out a tester's seed from the game data: points as the bots spend them, the promotion's trial, and for a capped one the best gear per slot and a stack of the best potions.
func plan(data *gamedata.Data, t tester) (seed, error) {
	start := data.StartClass()
	if t.stage == fresh {
		return seed{class: start, level: 1}, nil
	}
	future, ok := data.Class(t.future)
	if !ok || future.Promotion.Level < 1 {
		return seed{}, fmt.Errorf("%s: no promotion to %q in the data", t.username, t.future)
	}
	s := seed{class: start, level: future.Promotion.Level}
	if t.stage == capped {
		s.class, s.level = future, data.LevelCap()
	}
	rules := data.StatRules()
	s.unspent = (s.level - 1) * rules.PointsPerLevel
	s.stats = spend.Stats(s.unspent, start.BaseStats, future.Promotion.Stats, future.AutoAssign)
	own := progression.AddStats(start.BaseStats, s.stats)

	// Skill points go as a bot's do: the levels before the promotion toward it, the rest into the class's first skill.
	before := (min(s.level, future.Promotion.Level) - 1) * rules.SkillPointsPerLevel
	s.skills = spend.Skills(int32(before), nil, future.Promotion.Skills, spend.MainSkill(future, false)) //nolint:gosec // points are small
	if s.class.ID == future.ID {
		after := spend.Skills(int32((s.level-future.Promotion.Level)*rules.SkillPointsPerLevel), s.skills, nil, spend.MainSkill(future, true)) //nolint:gosec // points are small
		for id, n := range after {
			s.skills[id] += n
		}
	}
	s.unspentSkills = (s.level - 1) * rules.SkillPointsPerLevel

	for _, id := range future.Promotion.Quests {
		q, ok := data.Quest(id)
		if !ok {
			return seed{}, fmt.Errorf("%s: no quest %q in the data", t.username, id)
		}
		if t.stage == capped {
			s.quests = append(s.quests, &characterv1.Quest{QuestId: id, Status: characterv1.QuestStatus_QUEST_STATUS_FINISHED, TimesFinished: 1})
			continue
		}
		done := make([]int32, len(q.Objectives))
		for k, o := range q.Objectives {
			done[k] = int32(o.Count) //nolint:gosec // counts are small
		}
		s.quests = append(s.quests, &characterv1.Quest{QuestId: id, Status: characterv1.QuestStatus_QUEST_STATUS_ACTIVE, Progress: done})
	}

	// Full HP as the world finds it at the first join, before a point is spent or gear worn; those raise the max, which regen fills. Full energy too, so a tester's skills work at once.
	d := progression.DeriveStats(rules, s.class.ID, s.level, start.BaseStats, progression.ItemStats{})
	s.hp, s.energy = d.MaxHP, data.Energy().Max
	if t.stage == capped {
		s.gear = gearFor(data, s.level, s.class, own)
		s.potions = potions(data)
	}
	return s, nil
}

// gearFor is the best item each equip slot can take: the highest level band the character meets, then the most stars, then the first in the file; rolled at the middle of each range, as a trainer's gift is.
func gearFor(data *gamedata.Data, level int, class gamedata.Class, own gamedata.Stats) []piece {
	var out []piece
	for _, slot := range data.EquipSlots() {
		var best *gamedata.Item
		for _, it := range data.Items() {
			if it.Tab != gamedata.Equip || it.Slot != slot.Kind || progression.Meets(it, level, class, own) != progression.NothingUnmet {
				continue
			}
			if best == nil || it.MinLevel > best.MinLevel || (it.MinLevel == best.MinLevel && it.Stars > best.Stars) {
				best = &it
			}
		}
		if best != nil {
			roll := progression.Rolled(best.Rolls, func(r gamedata.Range) int { return (r.Min + r.Max) / 2 })
			out = append(out, piece{template: best.ID, slot: slot.ID, quantity: 1, roll: &roll})
		}
	}
	return out
}

// potions are a full stack of the potion that heals the most HP.
func potions(data *gamedata.Data) []piece {
	var best *gamedata.Item
	for _, it := range data.Items() {
		if it.Usable() && (best == nil || it.HealHP > best.HealHP) {
			best = &it
		}
	}
	if best == nil {
		return nil
	}
	return []piece{{template: best.ID, quantity: best.MaxStack}}
}
