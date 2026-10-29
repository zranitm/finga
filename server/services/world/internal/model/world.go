// Package model is the world service's model (Low-level design, page 11):
// what a joining player and a loaded character carry, what the player sees
// (views and the events delivered each tick), the checkpoints and level-ups
// it saves, pickups and their outcomes, the combat rules (combat.go), and
// the errors its use cases return. It needs nothing but data; the map
// instance's own state while it ticks belongs to the application.
package model

import (
	"errors"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"

	"github.com/google/uuid"
)

// The world's errors.
var (
	ErrBusy    = errors.New("world: map instance inbox is full")
	ErrNoMap   = errors.New("world: no such map")
	ErrStopped = errors.New("world: instance is not running")
	// ErrClassUnavailable is a joining character whose class the game data
	// does not have: a join that cannot happen, never a player's refusal.
	ErrClassUnavailable = errors.New("world: class not available")
)

// Player is what an instance needs to know about a character when it
// joins; movement and combat state are the instance's own from then on.
// Derived stats are computed by the instance from Class, Stats and Level.
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
	// Connection is the gateway connection the player joined through; a
	// Leave naming another connection leaves the player on the map.
	Connection string
}

// Character is a character as the world loads it to join: the state the
// character service saved last.
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

// View is the character as the player sees it once joined: Welcome's
// character, with the maximums derived from class, stats and level.
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
	// Invulnerable is set while the second after contact damage lasts
	// (step 19); its edges are carried in Deltas even without movement.
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
type Event interface{ event() }

// Snapshot is the whole map, sent to a player on join.
type Snapshot struct {
	Tick    uint64
	MapID   string
	Players []PlayerView
	Mobs    []MobView
	Drops   []DropView
}

// Delta is what changed in one tick, for one player: AckSeq is the latest
// input sequence applied for that player, Players and Mobs the ones that
// moved, changed or appeared, Removed the ones that left.
type Delta struct {
	Tick         uint64
	AckSeq       uint32
	Players      []PlayerView
	Mobs         []MobView
	Drops        []DropView // new on the ground
	Removed      []kernel.CharacterID
	RemovedMobs  []uint64
	RemovedDrops []uint64
}

// Teleport moves the player to another map: a respawn away from the town
// or a portal (step 14). The target instance sends the Snapshot.
type Teleport struct {
	Tick  uint64
	MapID string
	X, Y  float64
}

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

// Respawned says a character is back, to everyone on the map it left and
// to the character.
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
}

func (Snapshot) event() {}

func (Delta) event() {}

func (Teleport) event() {}

func (Damage) event() {}

func (Died) event() {}

func (Respawned) event() {}

func (ExpGained) event() {}

func (LevelUp) event() {}

// Checkpoint is a character's non-economic state, saved every 30 seconds
// (TD-02) and on map change and logout.
type Checkpoint struct {
	ID    kernel.CharacterID
	MapID string // the map it is on, or moving to
	X, Y  float64
	HP    int
	MP    int
	Exp   int64
}

// LevelUpSave is saved right away when a character gains a level.
type LevelUpSave struct {
	ID    kernel.CharacterID
	Level int
	Exp   int64
	Stats gamedata.Stats
	HP    int
	MP    int
}

// Job is one unit of work for the persistence worker: exactly one field set.
type Job struct {
	Checkpoint *Checkpoint
	LevelUp    *LevelUpSave
}

// DropView is an item on the ground as everyone sees it.
type DropView struct {
	ID       uint64
	Template string
	Quantity int
	X, Y     float64
}

// InventoryChanged tells the player which slots a committed change touched.
type InventoryChanged struct {
	Tick  uint64
	Slots []Slot
}

// Slot is one inventory slot a committed pickup touched; Item is nil when
// the slot emptied.
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
}

// PickupFailed tells the player why a pickup did not happen; the drop
// stays on the ground. Err is the item service's refusal (a full
// inventory) as Pickups returned it, or one of the world's own errors
// below; the gateway turns it into a Response.
type PickupFailed struct {
	Tick uint64
	Err  error
}

// The pickup errors the world raises itself. Each names one failure; the
// service's handler sends it as a reason, and the gateway gives it a code
// and the words a player sees (its errmap).
var (
	ErrPickupsUnavailable = errors.New("world: pickups not wired")
	ErrPickupBusy         = errors.New("world: pickup workers busy")
)

func (InventoryChanged) event() {}

func (PickupFailed) event() {}

// Pickup is one pickup for the item service: who picks up, and the drop's
// contents. The drop's ID is the idempotency key and the cause, so a
// retried pickup can only ever grant the drop once.
type Pickup struct {
	Account   kernel.AccountID // scopes the idempotency key
	Character kernel.CharacterID
	DropID    uuid.UUID
	Template  string
	Quantity  int
}
