// Package gamedata loads and validates the content in data/ and serves it read-only.
package gamedata

import (
	"slices"
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

// DamageType is what an attack deals, and so which defense meets it.
type DamageType string

// The damage types.
const (
	Physical DamageType = "physical"
	Magic    DamageType = "magic"
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

// Weapon is the slot kind of weapons, the only equipment that may keep a class.
const Weapon SlotKind = "weapon"

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

// Rolls are the range of each stat an equipment item gives, the five primary and the sixteen derived; fields, not a map, so every sum runs in one order.
type Rolls struct {
	STR, DEX, INT, AGI, VIT                                    Range
	PhysicalAttack, MagicAttack, PhysicalDefense, MagicDefense Range
	MaxHP, MaxMP, Accuracy, Evasion                            Range
	CritRate, CritDamage, Penetration, HPRegen, MPRegen        Range
	AttackSpeed, CooldownSpeed, MoveSpeed                      Range
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
	STR int `yaml:"str" json:"str"`
	DEX int `yaml:"dex" json:"dex"`
	INT int `yaml:"int" json:"int"`
	AGI int `yaml:"agi" json:"agi"`
	VIT int `yaml:"vit" json:"vit"`
}

// Total is the sum of the five stats.
func (s Stats) Total() int { return s.STR + s.DEX + s.INT + s.AGI + s.VIT }

// Primary names one of the five primary stats.
type Primary int

// The primary stats, in their fixed order.
const (
	STR Primary = iota
	DEX
	INT
	AGI
	VIT
)

// Of is one primary stat's value.
func (s Stats) Of(p Primary) int {
	return [...]int{s.STR, s.DEX, s.INT, s.AGI, s.VIT}[p]
}

// PerPrimary holds one value per primary stat.
type PerPrimary[T any] struct {
	STR, DEX, INT, AGI, VIT T
}

// Flats holds one value per flat derived stat, the ones a point adds a fixed amount to; fields, not a map, so every sum runs in one order.
type Flats[T any] struct {
	PhysicalAttack, MagicAttack, PhysicalDefense, MagicDefense    T
	MaxHP, MaxMP, Accuracy, Evasion, CritDamage, HPRegen, MPRegen T
}

// Rates holds one value per rate with diminishing returns.
type Rates[T any] struct {
	CritRate, AttackSpeed, CooldownSpeed, MoveSpeed T
}

// Grows is a class's base for a flat stat at level 1, and what each level after adds.
type Grows struct {
	Base, PerLevel float64
}

// At is the base at level.
func (g Grows) At(level int) float64 { return g.Base + g.PerLevel*float64(level-1) }

// Curve is a rate with diminishing returns over one primary stat: base + (limit − base) × points ÷ (points + k).
type Curve struct {
	Stat           Primary
	Base, Limit, K float64
}

// StatRules are data/stats.yaml: what each point gives, every class alike, each class's bases, and the limits.
type StatRules struct {
	PointsPerLevel int
	PerPoint       PerPrimary[Flats[float64]]
	Curves         Rates[Curve]
	Penetration    float64 // its limit; only gear gives it
	MinHit, MaxHit float64 // what the hit chance is held between
	Classes        map[kernel.Class]Flats[Grows]
}

// Attack is a class's basic attack: its shape and what it deals.
type Attack struct {
	Kind     AttackKind
	Damage   DamageType
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
	ID           kernel.Class
	Name         string
	Attack       Attack
	BaseStats    Stats // primary stats at level 1
	AutoAssign   Stats // how Auto-assign spends a level's points
	StarterItems []Grant
}

// Item is an item template.
type Item struct {
	ID          string
	Name        string
	Tab         Tab
	Stars       int // 1 to 7, the same for every copy
	Description string
	Slot        SlotKind     // equipment only
	Class       kernel.Class // weapons only; empty: any class
	MinLevel    int
	Requires    Stats // equipment only: the least of each primary stat a character's own stats must reach
	Rolls       Rolls // equipment only: each copy rolls its stats in these ranges when it drops
	HealHP      int   // consumables
	HealMP      int
	MaxStack    int // 1 for equipment, 100 for the rest
}

// Usable reports whether the item is used from the Use tab: a potion, restoring HP or MP.
func (it Item) Usable() bool { return it.Tab == Use && (it.HealHP > 0 || it.HealMP > 0) }

// Drop is one entry of a mob's drop table; each rolls independently.
type Drop struct {
	Item   string
	Chance float64 // (0, 1]
	Min    int
	Max    int
}

// Mob is a mob type.
type Mob struct {
	ID              string
	Name            string
	Level           int
	Elite           bool // a map's bigger, rarer mob, with more HP, EXP and drops than its normal one
	HP              int
	Attack          int
	Damage          DamageType // what its contact attack deals
	PhysicalDefense int
	MagicDefense    int
	Accuracy        int
	Evasion         int
	Exp             int64
	Speed           float64 // px/s
	Width           float64
	Height          float64
	Behavior        string
	Drops           []Drop
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
	Name        string // what players see, as the banner on entering
	Kind        MapKind
	Start       bool   // new characters start here; exactly one town has it
	Town        string // the town its players respawn in; a town names itself
	MinLevel    int    // a hunting map's mobs are of these levels; 0 for a town
	MaxLevel    int
	Width       float64 // pixels
	Height      float64
	Geometry    sim.Geometry
	Portals     []Portal
	Spawns      []Spawn
	PlayerSpawn Point // where players appear; in towns, the respawn point
	NPCs        []Placement
}

// Placement is an NPC standing on a map, from its npcs layer.
type Placement struct {
	NPC string
	At  Point
}

// Loot is how drops behave on the ground.
type Loot struct {
	CollectDelay   time.Duration // from the kill until a drop flies to its owner
	DropLife       time.Duration // a drop still on the ground then is lost
	RoomCheckEvery time.Duration // how often an owner's drops waiting for room are tried again
}

// Data is everything loaded, read-only after Load.
type Data struct {
	movement    sim.Params
	loot        Loot
	classes     []Class
	classIdx    map[kernel.Class]int
	stats       StatRules
	items       []Item
	itemIdx     map[string]int
	slots       []EquipSlot
	mobs        []Mob
	mobIdx      map[string]int
	maps        []*Map
	mapIdx      map[string]int
	start       *Map
	levelCap    int
	expToNext   []int64 // index level-1, for levels 1..cap-1
	npcs        []NPC
	npcIdx      map[string]int
	talkReach   float64
	quests      []Quest
	questIdx    map[string]int
	maxActive   int
	chatLineMax int
}

// Movement is the simulation's constants.
func (d *Data) Movement() sim.Params { return d.movement }

// Loot is how drops behave on the ground.
func (d *Data) Loot() Loot { return d.loot }

// ChatLineMax is the most characters a chat line holds, after cleaning.
func (d *Data) ChatLineMax() int { return d.chatLineMax }

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

// StatRules are what each point gives, each class's bases, and the limits.
func (d *Data) StatRules() StatRules { return d.stats }

// Item finds an item template.
func (d *Data) Item(id string) (Item, bool) {
	i, ok := d.itemIdx[id]
	if !ok {
		return Item{}, false
	}
	return d.items[i], true
}

// EquipSlots lists the slots a character wears equipment in, in file order.
func (d *Data) EquipSlots() []EquipSlot { return d.slots }

// EquipSlot finds an equip slot.
func (d *Data) EquipSlot(id string) (EquipSlot, bool) {
	k := slices.IndexFunc(d.slots, func(s EquipSlot) bool { return s.ID == id })
	if k < 0 {
		return EquipSlot{}, false
	}
	return d.slots[k], true
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

// StartTown is the town new characters start in, the one map with the property start.
func (d *Data) StartTown() *Map { return d.start }

// LevelCap is the highest level.
func (d *Data) LevelCap() int { return d.levelCap }

// ExpToNext is the EXP from level to level+1; false at the cap or for a level that doesn't exist.
func (d *Data) ExpToNext(level int) (int64, bool) {
	if level < 1 || level >= d.levelCap {
		return 0, false
	}
	return d.expToNext[level-1], true
}
