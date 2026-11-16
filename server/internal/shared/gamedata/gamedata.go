// Package gamedata is the game's content and its read-only queries; package load reads and checks it from data/.
package gamedata

import (
	"finga/internal/kernel"
	"finga/internal/sim"
)

// DamageType is what an attack deals, and so which defense meets it.
type DamageType string

// The damage types.
const (
	Physical DamageType = "physical"
	Magic    DamageType = "magic"
)

// Parts are the checked content New builds Data from, each list in file order.
type Parts struct {
	Movement        sim.Params
	Loot            Loot
	Energy          Energy
	Classes         []Class
	Skills          []Skill
	StatRules       StatRules
	LevelCap        int
	ExpToNext       []int64 // index level-1, for levels 1..cap-1
	Items           []Item
	EquipSlots      []EquipSlot
	Mobs            []Mob
	MobRules        MobRules
	FieldBoss       FieldBossRules
	Maps            []*Map
	StartTown       *Map
	NPCs            []NPC
	TalkReach       float64
	Quests          []Quest
	MaxActiveQuests int
	ChatLineMax     int
}

// Data is everything loaded, read-only once built.
type Data struct {
	p        Parts
	classIdx map[kernel.Class]int
	skillIdx map[string]int
	itemIdx  map[string]int
	mobIdx   map[string]int
	mapIdx   map[string]int
	npcIdx   map[string]int
	questIdx map[string]int
}

// New indexes parts by ID; they must already be checked, as load.Dir does.
func New(p Parts) *Data {
	return &Data{
		p:        p,
		classIdx: index(p.Classes, func(c Class) kernel.Class { return c.ID }),
		skillIdx: index(p.Skills, func(s Skill) string { return s.ID }),
		itemIdx:  index(p.Items, func(it Item) string { return it.ID }),
		mobIdx:   index(p.Mobs, func(m Mob) string { return m.ID }),
		mapIdx:   index(p.Maps, func(m *Map) string { return m.ID }),
		npcIdx:   index(p.NPCs, func(n NPC) string { return n.ID }),
		questIdx: index(p.Quests, func(q Quest) string { return q.ID }),
	}
}

// index maps each entry's ID to its position in all.
func index[K comparable, T any](all []T, id func(T) K) map[K]int {
	idx := make(map[K]int, len(all))
	for i, v := range all {
		idx[id(v)] = i
	}
	return idx
}

// find is the entry idx files under id.
func find[K comparable, T any](all []T, idx map[K]int, id K) (T, bool) {
	i, ok := idx[id]
	if !ok {
		var zero T
		return zero, false
	}
	return all[i], true
}

// Movement is the simulation's constants.
func (d *Data) Movement() sim.Params { return d.p.Movement }

// ChatLineMax is the most characters a chat line holds, after cleaning.
func (d *Data) ChatLineMax() int { return d.p.ChatLineMax }
