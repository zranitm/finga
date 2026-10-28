// Package world runs the map instances: one actor per map that ticks at
// 20 Hz, accepts players, simulates their movement from inputs (TD-18),
// runs the mobs and resolves combat, and sends each player what changed.
// It has no repository; it reaches the database only through other
// modules' services, from worker goroutines, never inside a tick. Its
// result types are its own; the gateway turns them into protocol messages.
package world

import (
	"context"
	"errors"
	"time"

	"finga/internal/gamedata"
	"finga/internal/model"
	"finga/internal/sim"

	"github.com/google/uuid"
)

// The module's errors.
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
	ID      model.CharacterID
	Account model.AccountID // scopes the idempotency keys of its actions
	Name    string
	Class   model.Class
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
	ID      model.CharacterID
	Account model.AccountID
	Name    string
	Class   model.Class
	Level   int
	Exp     int64
	Stats   gamedata.Stats
	HP      int
	MP      int
	MapID   string
	X, Y    float64
}

// Characters loads a character for a join; an adapter over the character
// service implements it. A character that is not the account's fails the
// way the character service says (not found).
type Characters interface {
	Load(ctx context.Context, id model.CharacterID, account model.AccountID) (Character, error)
}

// Starter grants a character its starter items once; an adapter over the
// item service implements it. The world calls it on every join as the
// safety net for a character created while the grant could not reach the
// item service (page 10).
type Starter interface {
	GrantStarter(ctx context.Context, account model.AccountID, id model.CharacterID, class model.Class) error
}

// View is the character as the player sees it once joined: Welcome's
// character, with the maximums derived from class, stats and level.
type View struct {
	ID    model.CharacterID
	Name  string
	Class model.Class
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
	ID    model.CharacterID
	Name  string
	Class model.Class
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
	Player model.CharacterID // zero when the entity is a mob
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
	Removed      []model.CharacterID
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
	ID   model.CharacterID
	X, Y float64
}

// Respawned says a character is back, to everyone on the map it left and
// to the character.
type Respawned struct {
	Tick  uint64
	ID    model.CharacterID
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
	ID    model.CharacterID
	Name  string
	Class model.Class
	Level int
	Exp   int64
	Stats gamedata.Stats
	HP    int
	MaxHP int
	MP    int
	MaxMP int
}

func (Snapshot) event()  {}
func (Delta) event()     {}
func (Teleport) event()  {}
func (Damage) event()    {}
func (Died) event()      {}
func (Respawned) event() {}
func (ExpGained) event() {}
func (LevelUp) event()   {}

// Sink is where an instance sends a player's events. Send never blocks: the
// gateway enqueues on the connection and returns false when the queue is
// full, and the instance then drops that player, never the tick.
type Sink interface {
	Send(Event) bool
}

// Checkpoint is a character's non-economic state, saved every 30 seconds
// (TD-02) and on map change and logout.
type Checkpoint struct {
	ID    model.CharacterID
	MapID string // the map it is on, or moving to
	X, Y  float64
	HP    int
	MP    int
	Exp   int64
}

// LevelUpSave is saved right away when a character gains a level.
type LevelUpSave struct {
	ID    model.CharacterID
	Level int
	Exp   int64
	Stats gamedata.Stats
	HP    int
	MP    int
}

// Saver persists checkpoints and level-ups; an adapter over the character
// service implements it. It is called from the persistence worker, never
// from a tick.
type Saver interface {
	SaveCheckpoint(ctx context.Context, cp Checkpoint) error
	SaveLevelUp(ctx context.Context, lu LevelUpSave) error
	// Retryable reports whether a failed save may succeed when tried again:
	// a transient failure, not a rule it ran into.
	Retryable(err error) bool
}

// Job is one unit of work for the persistence worker: exactly one field set.
type Job struct {
	Checkpoint *Checkpoint
	LevelUp    *LevelUpSave
}

// Mover moves a player to another map's instance after a respawn or a
// portal; the World implements it. It must not block the calling tick.
type Mover interface {
	Transfer(p Player, sink Sink, mapID string)
}

// TickObserver sees each tick's duration against its budget;
// internal/telemetry implements it.
type TickObserver interface {
	ObserveTick(mapID string, took, budget time.Duration)
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
	ID       model.ItemID
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
// handlers give it a code and the words a player sees (internal/handler/errmap).
var (
	ErrPickupsUnavailable = errors.New("world: pickups not wired")
	ErrPickupBusy         = errors.New("world: pickup workers busy")
)

func (InventoryChanged) event() {}
func (PickupFailed) event()     {}

// Pickup is one pickup for the item service: who picks up, and the drop's
// contents. The drop's ID is the idempotency key and the cause, so a
// retried pickup can only ever grant the drop once.
type Pickup struct {
	Account   model.AccountID // scopes the idempotency key
	Character model.CharacterID
	DropID    uuid.UUID
	Template  string
	Quantity  int
}

// Pickups is what the world needs from the item service; an adapter over
// its client implements it. Called from economy workers, never inside a
// tick.
type Pickups interface {
	// Add puts a drop in the character's inventory and returns the slots it
	// changed.
	Add(ctx context.Context, p Pickup) ([]Slot, error)
	// Retryable reports whether a failed Add may succeed when tried again
	// with the same drop: a transient failure, not a rule the pickup ran
	// into (a full inventory).
	Retryable(err error) bool
}
