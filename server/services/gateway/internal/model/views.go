package model

// PlayerView is a player as everyone on its map sees it.
type PlayerView struct {
	ID, Name, Class    string
	Level              int
	X, Y, VX, VY       float64
	Facing             int // -1 left, 1 right
	OnGround, Climbing bool
	HP, MaxHP          int
	Dead               bool
	Invulnerable       bool // for a second after contact damage
}

// MobView is a mob as the players on its map see it; its ID counts up within one map instance.
type MobView struct {
	ID        uint64
	Type      string
	X, Y      float64
	Facing    int
	HP, MaxHP int
}

// DropView is an item on the ground; its ID counts up within one map instance.
type DropView struct {
	ID       uint64
	Template string
	Quantity int
	X, Y     float64
}

// CharacterView is the player's own character.
type CharacterView struct {
	ID, Name, Class string
	Level           int
	Exp             int64
	Stats           Stats
	HP, MaxHP       int
	MP, MaxMP       int
}

// ItemView is an item in the player's inventory.
type ItemView struct {
	ID, Template, Tab string
	Slot, Quantity    int
}

// Inventory is the player's items as they stood at Revision, the count of the inventory's changes.
type Inventory struct {
	Revision uint64
	Items    []ItemView
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

// PartyMember is one member of a party, online or not.
type PartyMember struct {
	CharacterID, Name string
	Level, HP, MaxHP  int
	MapID             string
	Online            bool
}

// Entity is a player (Character set) or a mob.
type Entity struct {
	Character string
	Mob       uint64
}

// ChatChannel is where a chat line goes; the zero value is no channel, which the social service refuses.
type ChatChannel int

// The chat channels.
const (
	ChatMap ChatChannel = iota + 1
	ChatParty
)
