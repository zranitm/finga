package account

import (
	"time"

	"finga/internal/shared/progression"
)

// CharacterView is the player's own character.
type CharacterView struct {
	ID, Name, Class string
	Level           int
	Exp             int64
	Stats           Stats
	HP, MaxHP       int
	Energy          int // as it joins; each Delta carries it after
	Sheet           StatSheet
}

// StatSheet is the character's stats with what it wears, and the equipment's share of each.
type StatSheet struct {
	Primary, PrimaryWorn, PrimarySkills Stats
	Derived, DerivedWorn, DerivedSkills DerivedStats
	Points                              Points
	Skills                              SkillPoints
	AttackLengthTicks                   int
	Buffs                               []BuffView
}

// BuffView is a buff running on the character: its skill, what's left of it as the sheet went out, the points it was used with, and who used it, "" for the character's own.
type BuffView struct {
	Skill  string
	Left   time.Duration
	Points int
	From   string
}

// SkillPoints are the skill points: unspent, invested in each skill by ID, and each skill's chosen trait, one on its default not listed.
type SkillPoints struct {
	Unspent  int
	Invested map[string]int
	Traits   map[string]string
}

// Points are the stat points: unspent, spent in each stat, and whether the free reset is left.
type Points struct {
	Unspent       int
	Spent         Stats
	FreeResetLeft bool
}

// DerivedStats are the computed stats: rates, crit damage and penetration as fractions, regen in points a second.
type DerivedStats struct {
	PhysicalAttack, MagicAttack, PhysicalDefense, MagicDefense float64
	Accuracy, Evasion, CritRate, CritDamage, Penetration       float64
	HPRegen                                                    float64
	CooldownSpeed, MoveSpeed                                   float64
	MaxHP                                                      int
}

// ItemView is an item in the player's inventory, or a worn one, which is in no tab.
type ItemView struct {
	ID, Template, Tab string
	Slot, Quantity    int
	Stats             *ItemStats // equipment's roll; nil for anything else
}

// ItemStats are what one equipment item gives: the five primary stats and the thirteen derived, rates, crit damage and penetration in tenths of a percent, regen in tenths of a point a second.
type ItemStats = progression.ItemStats

// EquipSlot is an equip slot and what it holds; Item is nil when it is empty.
type EquipSlot struct {
	Slot string
	Item *ItemView
}

// Inventory is the player's items and what it wears as they stood at Revision, the count of their changes.
type Inventory struct {
	Revision  uint64
	Items     []ItemView
	Equipment []EquipSlot
}

// InventorySlot is a slot an inventory command changed; Item is nil when the slot is now empty.
type InventorySlot struct {
	Tab  string
	Slot int
	Item *ItemView
}

// PartyView is the player's party.
type PartyView struct {
	Leader  string
	Members []PartyMember
}

// Channels are a region's open channels and how full each is, against the players a channel takes before the next opens.
type Channels struct {
	Open []ChannelLoad
	Cap  int
}

// ChannelLoad is a channel's number and its players, those on their way included.
type ChannelLoad struct {
	Channel int
	Players int
}

// AreaBoss is a region's hunting area's field boss: up, or beaten and back in BackInMs.
type AreaBoss struct {
	Area     string
	Up       bool
	BackInMs int64
}

// PartyMember is one member of a party, online or not.
type PartyMember struct {
	CharacterID, Name, Class string
	Level, HP, MaxHP         int
	MapID                    string
	Channel                  int // the region's channel, from 1; 0 off regions
	Online                   bool
}

// ChatChannel is where a chat line goes; the zero value is no channel, which the social module refuses.
type ChatChannel int

// The chat channels.
const (
	ChatMap ChatChannel = iota + 1
	ChatParty
)

// Stats are a character's five primary stats.
type Stats struct {
	STR, DEX, INT, AGI, VIT int
}
