// Package model holds the world service's data: players, views, events and combat rules.
package model

import (
	"errors"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/progression"
	"finga/internal/sim"

	"github.com/google/uuid"
)

// The world's errors.
var (
	ErrBusy    = errors.New("world: map instance inbox is full")
	ErrNoMap   = errors.New("world: no such map")
	ErrStopped = errors.New("world: instance is not running")
	// ErrClassUnavailable means the game data lacks the class: a server fault, not a refusal.
	ErrClassUnavailable = errors.New("world: class not available")
	ErrCharacterDead    = errors.New("world: a dead character can't change what it wears")
)

// Worn is what a character wears as the item service last counted it: the sum of the worn items' rolls, at the inventory's Revision.
type Worn struct {
	Revision uint64
	Stats    progression.ItemStats
}

// Player is what an instance needs about a joining character.
type Player struct {
	ID      kernel.CharacterID
	Account kernel.AccountID // scopes the idempotency keys of its actions
	Name    string
	Class   kernel.Class
	Level   int
	Exp     int64
	Stats   gamedata.Stats
	HP      int
	MP      int
	X, Y    float64 // where to appear: feet centre
	// Connection is the gateway connection joined through; a Leave from another one is ignored.
	Connection string
	Worn       Worn // travels with the player between maps
}

// Character is a character as last saved, loaded to join.
type Character struct {
	ID      kernel.CharacterID
	Account kernel.AccountID
	Name    string
	Class   kernel.Class
	Level   int
	Exp     int64
	Stats   gamedata.Stats
	HP      int
	MP      int
	MapID   string
	X, Y    float64
}

// View is the joined player's own character, with derived maximums.
type View struct {
	ID    kernel.CharacterID
	Name  string
	Class kernel.Class
	Level int
	Exp   int64
	Stats gamedata.Stats
	HP    int
	MaxHP int
	MP    int
	MaxMP int
	MapID string
	X, Y  float64
	Sheet progression.Sheet
	// JoinSeq rises with each join, across restarts too, so the character's later login has the higher one.
	JoinSeq uint64
}

// PlayerView is a player as everyone on the map sees it.
type PlayerView struct {
	ID    kernel.CharacterID
	Name  string
	Class kernel.Class
	Level int
	HP    int
	MaxHP int
	Dead  bool
	// Invulnerable lasts a second after contact damage; Deltas carry its edges.
	Invulnerable bool
	State        sim.State
}

// MobView is a mob as everyone on the map sees it.
type MobView struct {
	ID     uint64
	Type   string
	X, Y   float64 // feet centre
	Facing int
	HP     int
	MaxHP  int
}

// EntityRef names a player or a mob in a combat event.
type EntityRef struct {
	Player kernel.CharacterID // zero when the entity is a mob
	Mob    uint64
}

// IsMob reports whether the reference names a mob.
func (e EntityRef) IsMob() bool { return e.Player.IsZero() }

// Event is something an instance tells a player.
//
//sumtype:decl
type Event interface{ event() }

// Snapshot is the whole map, sent to a player on join.
type Snapshot struct {
	Tick    uint64
	MapID   string
	Players []PlayerView
	Mobs    []MobView
	Drops   []DropView
}

// Frame is what changed on a map in one tick, shared by every player there and read-only once sent.
type Frame struct {
	Tick         uint64
	Players      []PlayerView
	Mobs         []MobView
	Drops        []DropView // new on the ground
	Removed      []kernel.CharacterID
	RemovedMobs  []uint64
	RemovedDrops []uint64
}

// Recipient is a player a frame goes to, with AckSeq, the latest input applied for it.
type Recipient struct {
	Character  kernel.CharacterID
	Connection string
	AckSeq     uint32
}

// Teleport moves the player to another map, whose instance sends the Snapshot.
type Teleport struct {
	Tick  uint64
	MapID string
	X, Y  float64
}

// Detached tells a connection its character is on no map any more (a transfer failed); the gateway joins it again.
type Detached struct{}

// Damage is one resolved hit, sent to everyone on the map.
type Damage struct {
	Tick   uint64
	Source EntityRef
	Target EntityRef
	Amount int
	Crit   bool
	Miss   bool
}

// Died says a character fell, to everyone on the map.
type Died struct {
	Tick uint64
	ID   kernel.CharacterID
	X, Y float64
}

// Respawned says a character is back, to the map it left and to the character.
type Respawned struct {
	Tick  uint64
	ID    kernel.CharacterID
	MapID string
	X, Y  float64
	HP    int
}

// ExpGained is EXP awarded to the player, after the award.
type ExpGained struct {
	Tick   uint64
	Amount int64
	Exp    int64
}

// LevelUp carries the player's new level, stats and restored HP and MP.
type LevelUp struct {
	Tick  uint64
	ID    kernel.CharacterID
	Name  string
	Class kernel.Class
	Level int
	Exp   int64
	Stats gamedata.Stats
	HP    int
	MaxHP int
	MP    int
	MaxMP int
	Sheet progression.Sheet
}

// StatsChanged is the player's stats after what it wears changed.
type StatsChanged struct {
	Tick  uint64
	Sheet progression.Sheet
	HP    int
	MaxHP int
	MP    int
	MaxMP int
}

func (Snapshot) event() {}

func (StatsChanged) event() {}

func (Teleport) event() {}
func (Detached) event() {}

func (Damage) event() {}

func (Died) event() {}

func (Respawned) event() {}

func (ExpGained) event() {}

func (LevelUp) event() {}

// Checkpoint is a character's non-economic state, saved every interval and at once on a level-up, map change or leave.
type Checkpoint struct {
	ID    kernel.CharacterID
	MapID string // the map it is on, or moving to
	X, Y  float64
	HP    int
	MP    int
	Level int
	Exp   int64
	Stats gamedata.Stats
}

// DropView is an item on the ground as everyone sees it.
type DropView struct {
	ID       uint64
	Template string
	Quantity int
	X, Y     float64
}

// InventoryChanged tells the player which slots and equip slots a committed change touched, as they stood at the inventory's Revision.
type InventoryChanged struct {
	Tick      uint64
	Revision  uint64
	Slots     []Slot
	Equipment []EquipSlot
}

// Inventory is slots of a character's inventory as they stood at Revision, the item service's count of its changes.
type Inventory struct {
	Revision uint64
	Slots    []Slot
}

// Slot is one inventory slot a pickup touched; Item is nil when it emptied.
type Slot struct {
	Tab  gamedata.Tab
	Slot int
	Item *SlotItem
}

// SlotItem is the item in a slot after the change.
type SlotItem struct {
	ID       kernel.ItemID
	Template string
	Quantity int
	Stats    *progression.ItemStats // what it rolled; nil for anything but equipment
}

// EquipSlot is one equip slot a change touched; Item is nil when it emptied.
type EquipSlot struct {
	Slot string
	Item *SlotItem
}

// EquipCall wears an inventory item, checked against the character's live level and class; Key is the client's request ID.
type EquipCall struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
	Key       string
	Item      kernel.ItemID
	EquipSlot string // rings only
	Level     int
	Class     kernel.Class
}

// UnequipCall puts a worn item back in the Equip tab, in ToSlot or, nil, the first free slot.
type UnequipCall struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
	Key       string
	EquipSlot string
	ToSlot    *int
}

// Equipped is what an equip or unequip changed, with all that is worn after it.
type Equipped struct {
	Slots     []Slot
	Equipment []EquipSlot
	Worn      Worn
}

// PickupFailed says why a pickup failed; the drop stays on the ground.
type PickupFailed struct {
	Tick uint64
	Err  error
}

// The pickup errors the world raises itself.
var (
	ErrPickupsUnavailable = errors.New("world: pickups unavailable")
	ErrPickupBusy         = errors.New("world: pickup workers busy")
)

func (InventoryChanged) event() {}

func (PickupFailed) event() {}

// Pickup is one pickup for the item service; DropID is its idempotency key.
type Pickup struct {
	Account   kernel.AccountID // scopes the idempotency key
	Character kernel.CharacterID
	DropID    uuid.UUID
	Template  string
	Quantity  int
	Stats     *progression.ItemStats // equipment's roll, nil for anything else
}
