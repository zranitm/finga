package gamedata

import (
	"slices"

	"finga/internal/kernel"
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

// Item finds an item template.
func (d *Data) Item(id string) (Item, bool) { return find(d.p.Items, d.itemIdx, id) }

// EquipSlots lists the slots a character wears equipment in, in file order.
func (d *Data) EquipSlots() []EquipSlot { return d.p.EquipSlots }

// EquipSlot finds an equip slot.
func (d *Data) EquipSlot(id string) (EquipSlot, bool) {
	k := slices.IndexFunc(d.p.EquipSlots, func(s EquipSlot) bool { return s.ID == id })
	if k < 0 {
		return EquipSlot{}, false
	}
	return d.p.EquipSlots[k], true
}
