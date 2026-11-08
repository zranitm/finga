// Package gamedata loads and validates the content in data/ and serves it read-only.
package gamedata

import (
	"time"

	"finga/internal/kernel"
	"finga/internal/sim"
)

// AttackKind is how a class's basic attack finds its target.
type AttackKind string

// The attack kinds.
const (
	Melee  AttackKind = "melee"  // a box in front of the character
	Ranged AttackKind = "ranged" // a straight shot along the facing direction
)

// Tab is an inventory tab, which is also the item type.
type Tab string

// The three tabs.
const (
	Equip Tab = "equip"
	Use   Tab = "use"
	Etc   Tab = "etc"
)

// SlotKind is what an equipment item names as its slot; every equip slot of that kind takes it.
type SlotKind string

// EquipSlot is one of the slots a character wears equipment in.
type EquipSlot struct {
	ID   string
	Name string
	Kind SlotKind
}

// Range is what a stat rolls between, both ends included; the zero Range is a stat the item doesn't give.
type Range struct {
	Min, Max int
}

// Rolls are the range of each stat an equipment item gives; fields, not a map, so every sum runs in one order.
type Rolls struct {
	Attack, Defense, STR, DEX, INT, AGI, VIT, Accuracy, Evasion, CritRate, CritDamage, MaxHP, MaxMP Range
}

// MapKind separates safe towns from hunting maps.
type MapKind string

// The map kinds.
const (
	Town    MapKind = "town"
	Hunting MapKind = "hunting"
)

// Stats are the five primary stats.
type Stats struct {
	STR int `yaml:"str"`
	DEX int `yaml:"dex"`
	INT int `yaml:"int"`
	AGI int `yaml:"agi"`
	VIT int `yaml:"vit"`
}

// Total is the sum of the five stats.
func (s Stats) Total() int { return s.STR + s.DEX + s.INT + s.AGI + s.VIT }

// Coef is a coefficient per primary stat; fields, not a map, so every formula sums in one order.
type Coef struct {
	STR, DEX, INT, AGI, VIT float64
}

// Formula is one derived stat: base + Σ coef × stat + per_level × (level − 1), capped when Cap > 0.
type Formula struct {
	Base     float64
	PerLevel float64
	Coef     Coef
	Cap      float64 // 0: no cap
}

// Formulas are a class's nine derived stats.
type Formulas struct {
	MaxHP, MaxMP, AttackPower, Accuracy, Evasion, CritRate, CritDamage, Defense, Penetration Formula
}

// Attack is a class's basic attack shape.
type Attack struct {
	Kind     AttackKind
	Width    float64 // melee box in front of the character
	Height   float64
	Range    float64 // ranged shot length
	Cooldown time.Duration
}

// Grant is a starter item: a template and a quantity.
type Grant struct {
	Item     string `yaml:"item"`
	Quantity int    `yaml:"quantity"`
}

// Class is a playable class.
type Class struct {
	ID            kernel.Class
	Name          string
	Attack        Attack
	BaseStats     Stats // primary stats at level 1
	LevelUpPoints Stats
	StarterItems  []Grant
}

// Item is an item template.
type Item struct {
	ID          string
	Name        string
	Tab         Tab
	Stars       int // 1 to 7, the same for every copy
	Description string
	Slot        SlotKind     // equipment only
	Class       kernel.Class // equipment only; empty: any class
	MinLevel    int
	Rolls       Rolls // equipment only: each copy rolls its stats in these ranges when it drops
	HealHP      int   // consumables
	HealMP      int
	MaxStack    int // 1 for equipment, 100 for the rest
}

// Drop is one entry of a mob's drop table; each rolls independently.
type Drop struct {
	Item   string
	Chance float64 // (0, 1]
	Min    int
	Max    int
}

// Mob is a mob type.
type Mob struct {
	ID       string
	Name     string
	Level    int
	HP       int
	Attack   int
	Defense  int
	Accuracy int
	Evasion  int
	Exp      int64
	Speed    float64 // px/s
	Width    float64
	Height   float64
	Behavior string
	Drops    []Drop
}

// Point is a position in map pixels: a character's feet centre.
type Point struct {
	X, Y float64
}

// Portal leads to a named portal on another map.
type Portal struct {
	Name         string
	Area         sim.Rect
	TargetMap    string
	TargetPortal string
}

// Arrival is where a character arriving through p stands: its rectangle's bottom centre.
func (p Portal) Arrival() Point {
	return Point{X: p.Area.X + p.Area.W/2, Y: p.Area.Y + p.Area.H}
}

// Spawn keeps up to Max mobs of one type inside Area; a missing one returns after Respawn.
type Spawn struct {
	Mob     string
	Max     int
	Respawn time.Duration
	Area    sim.Rect
}

// Map is one Tiled map, converted.
type Map struct {
	ID          string
	Kind        MapKind
	Town        string  // the town its players respawn in; a town names itself
	Width       float64 // pixels
	Height      float64
	Geometry    sim.Geometry
	Portals     []Portal
	Spawns      []Spawn
	PlayerSpawn Point // where players appear; in towns, the respawn point
}

// Data is everything loaded, read-only after Load.
type Data struct {
	movement  sim.Params
	classes   []Class
	classIdx  map[kernel.Class]int
	formulas  map[kernel.Class]Formulas
	items     []Item
	itemIdx   map[string]int
	slots     []EquipSlot
	mobs      []Mob
	mobIdx    map[string]int
	maps      []*Map
	mapIdx    map[string]int
	levelCap  int
	expToNext []int64 // index level-1, for levels 1..cap-1
}

// Movement is the simulation's constants.
func (d *Data) Movement() sim.Params { return d.movement }

// Classes lists the playable classes in file order.
func (d *Data) Classes() []Class { return d.classes }

// Class finds a playable class.
func (d *Data) Class(id kernel.Class) (Class, bool) {
	i, ok := d.classIdx[id]
	if !ok {
		return Class{}, false
	}
	return d.classes[i], true
}

// Formulas are a class's derived-stat formulas.
func (d *Data) Formulas(id kernel.Class) (Formulas, bool) {
	f, ok := d.formulas[id]
	return f, ok
}

// Item finds an item template.
func (d *Data) Item(id string) (Item, bool) {
	i, ok := d.itemIdx[id]
	if !ok {
		return Item{}, false
	}
	return d.items[i], true
}

// Mobs lists every mob type in file order.
func (d *Data) Mobs() []Mob { return d.mobs }

// Mob finds a mob type.
func (d *Data) Mob(id string) (Mob, bool) {
	i, ok := d.mobIdx[id]
	if !ok {
		return Mob{}, false
	}
	return d.mobs[i], true
}

// Maps lists every map, in file-name order.
func (d *Data) Maps() []*Map { return d.maps }

// Map finds a map by ID (its file name without the extension).
func (d *Data) Map(id string) (*Map, bool) {
	i, ok := d.mapIdx[id]
	if !ok {
		return nil, false
	}
	return d.maps[i], true
}

// Town is the town map, where characters start and respawn.
func (d *Data) Town() *Map {
	for _, m := range d.maps {
		if m.Kind == Town {
			return m
		}
	}
	return nil
}

// LevelCap is the highest level.
func (d *Data) LevelCap() int { return d.levelCap }

// ExpToNext is the EXP from level to level+1; false at the cap or for a level that doesn't exist.
func (d *Data) ExpToNext(level int) (int64, bool) {
	if level < 1 || level >= d.levelCap {
		return 0, false
	}
	return d.expToNext[level-1], true
}
