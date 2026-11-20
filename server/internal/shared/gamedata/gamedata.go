// Package gamedata is the game's content and its read-only queries; package load reads and checks it from data/.
package gamedata

import (
	"math"
	"time"

	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
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
	Block           Block
	Combat          time.Duration
	Classes         []Class
	Skills          []Skill
	Statuses        []Status
	StatusRules     StatusRules
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
	TeleportChannel time.Duration
	Pvp             PvpRules
	Interest        Interest
	Pieces          []Piece
}

// Data is everything loaded, read-only once built.
type Data struct {
	p         Parts
	classIdx  map[kernel.Class]int
	skillIdx  map[string]int
	statusIdx map[string]int
	itemIdx   map[string]int
	mobIdx    map[string]int
	mapIdx    map[string]int
	npcIdx    map[string]int
	questIdx  map[string]int
	pieceIdx  map[string]int
	// pieceItemIdx files each piece with an item under its item.
	pieceItemIdx map[string]int
	// waypointIdx files each region's waypoints by id, their ids unique across every region.
	waypointIdx map[string]waypointAt
}

// waypointAt is where a waypoint is: its map's index in Maps, and its own in the map's Waypoints.
type waypointAt struct{ m, w int }

// New indexes parts by ID; they must already be checked, as load.Dir does.
func New(p Parts) *Data {
	return &Data{
		p:            p,
		classIdx:     index(p.Classes, func(c Class) kernel.Class { return c.ID }),
		skillIdx:     index(p.Skills, func(s Skill) string { return s.ID }),
		statusIdx:    index(p.Statuses, func(s Status) string { return s.ID }),
		itemIdx:      index(p.Items, func(it Item) string { return it.ID }),
		mobIdx:       index(p.Mobs, func(m Mob) string { return m.ID }),
		mapIdx:       index(p.Maps, func(m *Map) string { return m.ID }),
		npcIdx:       index(p.NPCs, func(n NPC) string { return n.ID }),
		questIdx:     index(p.Quests, func(q Quest) string { return q.ID }),
		pieceIdx:     index(p.Pieces, func(p Piece) string { return p.ID }),
		pieceItemIdx: pieceItems(p.Pieces),
		waypointIdx:  waypointsOf(p.Maps),
	}
}

// waypointsOf files every map's waypoints by id.
func waypointsOf(maps []*Map) map[string]waypointAt {
	idx := map[string]waypointAt{}
	for m, mp := range maps {
		for w, wp := range mp.Waypoints {
			idx[wp.ID] = waypointAt{m, w}
		}
	}
	return idx
}

// pieceItems files each piece an item holds under the item.
func pieceItems(all []Piece) map[string]int {
	idx := index(all, func(p Piece) string { return p.Item })
	delete(idx, "") // a piece no item holds
	return idx
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

// BotPathMargin is how much farther than a player's radius a bot's path keeps from walls where it can, as eight-way keys stray off its line; the loader checks the sum on the walk grid's steps.
const BotPathMargin = 0.25

// ChatLineMax is the most characters a chat line holds, after cleaning.
func (d *Data) ChatLineMax() int { return d.p.ChatLineMax }

// TeleportChannel is how long a teleport channels before it takes the player.
func (d *Data) TeleportChannel() time.Duration { return d.p.TeleportChannel }

// Interest is the cells a map is cut in and the block of them each player is sent (large-regions.md section 5).
type Interest struct {
	Cell                float64 // metres a side
	Behind, Ahead, Side int     // rows south and north of the player's cell, as the camera faces north, and columns to each side
}

// Cells is how many interest cells a map sizeX by sizeZ is across and up.
func (in Interest) Cells(sizeX, sizeZ float64) (nx, nz int) {
	return max(1, int(math.Ceil(sizeX/in.Cell))), max(1, int(math.Ceil(sizeZ/in.Cell)))
}

// Whole is whether one block holds a map nx by nz cells from every cell of it, so the map is sent whole (large-regions.md section 5).
func (in Interest) Whole(nx, nz int) bool { return nx-1 <= in.Side && nz-1 <= min(in.Behind, in.Ahead) }

// Interest is the cells and the block of them each player is sent.
func (d *Data) Interest() Interest { return d.p.Interest }
