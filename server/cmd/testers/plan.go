package main

import (
	"fmt"
	"slices"

	"finga/cmd/internal/spend"
	"finga/internal/shared/adminapi"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
)

// stage is how far a test character is taken.
type stage int

const (
	fresh      stage = iota // level 1, as created
	promotable              // at its class's promotion level with every condition met, its trial done but not handed in, with potions
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
	quests []adminapi.Quest
	// waypoints are every one of the regions a player reaches from the start, found.
	waypoints []string
	// unspent and unspentSkills are the level's points, which stats and skills then spend.
	unspent, unspentSkills int
	stats                  gamedata.Stats
	skills                 map[string]int32
	// main is the skill its spare points go to, first on its skill bar.
	main          string
	gear, potions []piece
}

// plan works out a tester's seed from the game data: points as the bots spend them, the promotion's trial, a stack of the best potions, and for a capped one the best gear per slot.
func plan(data *gamedata.Data, t tester) (seed, error) {
	start := data.StartClass()
	if t.stage == fresh {
		return seed{class: start, level: 1, waypoints: shippedWaypoints(data)}, nil
	}
	future, ok := data.Class(t.future)
	if !ok || future.Promotion.Level < 1 {
		return seed{}, fmt.Errorf("%s: no promotion to %q in the data", t.username, t.future)
	}
	s := seed{class: start, level: future.Promotion.Level, waypoints: shippedWaypoints(data)}
	if t.stage == capped {
		s.class, s.level = future, data.LevelCap()
	}
	rules := data.StatRules()
	s.unspent = (s.level - 1) * rules.PointsPerLevel
	s.stats = spend.Stats(s.unspent, start.BaseStats, future.Promotion.Stats, future.AutoAssign)
	own := progression.AddStats(start.BaseStats, s.stats)

	// Skill points go as a bot's do: the levels before the promotion toward it and a point into each skill they open, the rest into the class's first skill.
	reached := min(s.level, future.Promotion.Level)
	before := (reached - 1) * rules.SkillPointsPerLevel
	s.skills = spend.Skills(int32(before), nil, future.Promotion.Skills, spend.Opened(data, start.ID, reached), spend.MainSkill(future, false)) //nolint:gosec // points are small
	if s.class.ID == future.ID {
		after := spend.Skills(int32((s.level-future.Promotion.Level)*rules.SkillPointsPerLevel), s.skills, nil, spend.Opened(data, future.ID, s.level), spend.MainSkill(future, true)) //nolint:gosec // points are small
		for id, n := range after {
			s.skills[id] += n
		}
	}
	s.unspentSkills = (s.level - 1) * rules.SkillPointsPerLevel
	s.main = spend.MainSkill(future, s.class.ID == future.ID)

	for _, id := range future.Promotion.Quests {
		q, ok := data.Quest(id)
		if !ok {
			return seed{}, fmt.Errorf("%s: no quest %q in the data", t.username, id)
		}
		if t.stage == capped {
			s.quests = append(s.quests, adminapi.Quest{ID: id, Status: "finished", TimesFinished: 1})
			continue
		}
		done := make([]int, len(q.Objectives))
		for k, o := range q.Objectives {
			done[k] = o.Count
		}
		s.quests = append(s.quests, adminapi.Quest{ID: id, Status: "active", Progress: done})
	}

	// Full HP as the world allows at the first join, before a point is spent or gear worn; play drinks potions up to the max those raise. Full energy too, so skills work at once.
	d := progression.DeriveStats(rules, s.class.ID, s.level, start.BaseStats, progression.ItemStats{})
	s.hp, s.energy = d.MaxHP, data.Energy().Max
	if t.stage == capped {
		s.gear = gearFor(data, s.level, s.class, own)
	}
	s.potions = potions(data)
	return s, nil
}

// barToSet is the skill bar to give a character whose keys are all empty, from the skills it has points in: its main skill, then its class's chain roots in their order, then its other actives, its own class's before those of the classes above it, each in file order.
// Only an active skill that may sit on a key of its own goes, never a passive or a follow-up, nor one dealing the other damage than the class's attack, as a Magician's strike would; false when the character has a bar already or nothing to put on one.
func barToSet(data *gamedata.Data, class gamedata.Class, main string, skills map[string]int, current []string) ([]string, bool) {
	if slices.ContainsFunc(current, func(s string) bool { return s != "" }) {
		return nil, false
	}
	var order []string
	add := func(id string) {
		sk, ok := data.Skill(id)
		if !ok || skills[id] == 0 || sk.Kind != gamedata.Active || !class.Keyable(id) || slices.Contains(order, id) {
			return
		}
		if sk.Effect == gamedata.DamageEffect && sk.Stages[sk.StageAt(skills[id])].Damage != class.Attack.Damage {
			return
		}
		order = append(order, id)
	}
	add(main)
	for _, ch := range class.Chains {
		add(ch.Root)
	}
	for _, id := range class.Line {
		if c, ok := data.Class(id); ok {
			for _, skill := range c.Skills {
				add(skill)
			}
		}
	}
	if len(order) == 0 {
		return nil, false
	}
	bar := make([]string, len(current))
	copy(bar, order)
	return bar, true
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

// shippedWaypoints is every waypoint of the regions a player reaches from the start by gates, in the data's order: the shipped regions and their places below, never the test region or any other nothing leads to.
func shippedWaypoints(data *gamedata.Data) []string {
	reached := map[string]bool{data.StartTown().ID: true}
	for grew := true; grew; {
		grew = false
		for _, m := range data.Maps() {
			if !reached[m.ID] {
				continue
			}
			for _, p := range m.Portals {
				if !reached[p.TargetMap] {
					reached[p.TargetMap], grew = true, true
				}
			}
		}
	}
	var ids []string
	for _, m := range data.Maps() {
		if reached[m.ID] {
			for _, w := range m.Waypoints {
				ids = append(ids, w.ID)
			}
		}
	}
	return ids
}
