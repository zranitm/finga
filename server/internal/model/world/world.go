// Package world holds the world module's data: players, views, events and combat rules.
package world

import (
	"errors"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/progression"
	"finga/internal/shared/sim"
)

// The world's errors.
var (
	ErrBusy    = errors.New("world: map instance inbox is full")
	ErrNoMap   = errors.New("world: no such map")
	ErrStopped = errors.New("world: instance is not running")
	// ErrClassUnavailable means the game data lacks the class: a server fault, not a refusal.
	ErrClassUnavailable = errors.New("world: class not available")
	ErrCharacterDead    = errors.New("world: a dead character can't do that")
	ErrNpcOutOfReach    = errors.New("world: the NPC isn't on the map, or is out of reach")
	ErrQuestUnavailable = errors.New("world: the NPC doesn't give, or take back, that quest")
	ErrQuestUnmet       = errors.New("world: the character doesn't meet the quest's conditions")
	ErrQuestActive      = errors.New("world: the quest is already active")
	ErrQuestFinished    = errors.New("world: the one-time quest is already finished")
	ErrTooManyQuests    = errors.New("world: the most active quests are already taken")
	ErrQuestNotReady    = errors.New("world: the quest isn't active, or a kill, talk or level objective is left")
	ErrHPFull           = errors.New("world: an HP potion at full HP restores nothing")
	ErrNotUsable        = errors.New("world: the game data has no potion of that template")
	ErrPointsChanged    = errors.New("world: the unspent points are not the count expected")
	ErrNotEnoughPoints  = errors.New("world: not that many unspent points")
	ErrNoFreeReset      = errors.New("world: the free reset is used")
	ErrBadPoints        = errors.New("world: points must be at least one in all, and none negative")

	ErrSkillPointsChanged   = errors.New("world: the unspent skill points are not the count expected")
	ErrNotEnoughSkillPoints = errors.New("world: not that many unspent skill points")
	ErrSkillNotLearnable    = errors.New("world: the character's class, and every class above it, doesn't teach that skill")
	ErrSkillLocked          = errors.New("world: the character's level is below the one the skill takes its first point from")
	ErrBadSkillWay          = errors.New("world: a moving skill's way isn't a unit vector and a length of 0 or more")
	ErrBadSkillPoints       = errors.New("world: skill points must be at least one in all, and none negative")

	ErrSkillNotLearned  = errors.New("world: the character has no points in that skill, or its class can't learn it")
	ErrSkillPassive     = errors.New("world: a passive skill is always on and can't be used")
	ErrNotInTown        = errors.New("world: a damaging or status skill can't be used in a town")
	ErrSkillCoolingDown = errors.New("world: the skill is still cooling down")
	ErrNotEnoughEnergy  = errors.New("world: not enough energy for the skill")
	ErrSkillWaiting     = errors.New("world: another skill use already waits for the next action turn")
	ErrCantAct          = errors.New("world: a status keeps the character from acting")
	ErrSilenced         = errors.New("world: a status keeps the character from using skills")
	ErrChainNotReady    = errors.New("world: the skill isn't a follow-up of its key's root, or its condition didn't hold at the press")
	ErrNotControlled    = errors.New("world: a break-free skill goes only while its user holds a hard status")

	ErrTraitUnknown = errors.New("world: the skill has no trait with that id")
	ErrTraitLocked  = errors.New("world: the extra trait opens once the skill's points reach its last stage")
	ErrInCombat     = errors.New("world: a trait or a look can't change while the player is in combat")

	ErrPouchGone       = errors.New("world: the pouch is gone, empty or not the player's, or the item is taken or named twice")
	ErrPouchOutOfReach = errors.New("world: the pouch is farther than the player's reach")
	ErrTakeBusy        = errors.New("world: the item is being taken, or a take already waits for the player's last one")

	ErrPromotionUnmet   = errors.New("world: the character doesn't meet every condition of the promotion")
	ErrNotYourNextClass = errors.New("world: the NPC doesn't train that class, or its parent isn't the character's class")

	ErrTeleportUnknownMap = errors.New("world: no map has that id to teleport to")
	ErrTeleportSameMap    = errors.New("world: the character is already on that map")
	ErrTeleportDead       = errors.New("world: a dead character can't teleport")
	ErrTeleportBusy       = errors.New("world: a teleport is already channelling")

	ErrTeleportUnknownWaypoint = errors.New("world: no waypoint has that id to teleport to")
	ErrTeleportNotFound        = errors.New("world: the character hasn't found that waypoint, and it isn't in a region's main town")

	ErrNotARegion    = errors.New("world: the character isn't in a region, which alone has channels")
	ErrSameChannel   = errors.New("world: the character is already in that channel")
	ErrNoSuchChannel = errors.New("world: the region has no such channel open")
	ErrChannelFull   = errors.New("world: the channel is full")
)

// Worn is what a character wears as the item module last counted it: each worn item, at the inventory's Revision.
type Worn struct {
	Revision uint64
	Items    []WornItem
}

// Look is a character's five picks, each a piece id, "" keeping the class's own, at the count of its changes (outfits.md section 4).
type Look struct {
	Outfit       string
	Headwear     string
	Face         string
	Weapon       string
	Wings        string
	HideHeadwear bool
	Revision     uint64
}

// LookCall wears a look: the item module checks each pick against the account's wardrobe and the class's weapon kind.
type LookCall struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
	Key       string
	Look      Look
	Class     kernel.Class // the live class
}

// WornItem is one worn item: its template and roll.
type WornItem struct {
	Template string
	Stats    progression.ItemStats
}

// Points are a character's stat points: those spent in each stat, those left to spend, and whether the free reset is used.
type Points struct {
	Spent         gamedata.Stats
	Unspent       int
	FreeResetUsed bool
}

// Sheet is what the character window shows: the stats with the equipment's share, the points, and the basic attack's length in ticks, which the client's action clock counts; and the buffs running, which the player's frame shows.
type Sheet struct {
	progression.Sheet
	Points            Points
	Skills            Skills // a copy of the player's
	AttackLengthTicks uint64
	Buffs             []BuffView // by skill
}

// BuffView is a buff on the player as the sheet goes out: its skill, what's left of it, the points it was used with, and who used it, zero for the player's own.
type BuffView struct {
	Skill  string
	Left   time.Duration
	Points int
	From   kernel.CharacterID
}

// Player is what an instance needs about a joining character.
type Player struct {
	ID      kernel.CharacterID
	Account kernel.AccountID // scopes the idempotency keys of its actions
	Name    string
	Class   kernel.Class
	Level   int
	Exp     int64
	Points  Points
	Skills  Skills
	HP      int
	Energy  int     // 0 to the game data's max
	X, Y, Z float64 // where to appear, on the ground under (X, Z): feet centre in metres, y up
	Yaw     float32 // the way it faces as it appears, radians from north toward east
	// Connection is the gateway connection joined through; a Leave from another one is ignored.
	Connection string
	Worn       Worn        // travels with the player between maps
	Look       Look        // travels with the player too
	Quests     Quests      // travels with the player too
	Timers     SkillTimers // travels with the player too, and the checkpoint keeps it
	Waypoints  Waypoints   // travels with the player too, and the checkpoint keeps it
	// HandedIn are completions the item module handed in while the world missed the reply; the map's join finishes them.
	HandedIn []HandedIn
	// Back is where a player going into a match returns to; nil outside one.
	Back *Return
}

// Character is a character as last saved, loaded to join.
type Character struct {
	ID        kernel.CharacterID
	Account   kernel.AccountID
	Name      string
	Class     kernel.Class
	Level     int
	Exp       int64
	Points    Points
	Skills    Skills
	HP        int
	Energy    int
	MapID     string
	X, Y, Z   float64 // feet centre, y up
	Yaw       float32 // the way it faced
	Quests    Quests
	Timers    SkillTimers // as the last checkpoint left them; some may have ended since
	Waypoints Waypoints
}

// View is the joined player's own character, with derived maximums.
type View struct {
	ID     kernel.CharacterID
	Name   string
	Class  kernel.Class
	Level  int
	Exp    int64
	Stats  gamedata.Stats // own: the class's starting stats plus the points
	HP     int
	MaxHP  int
	Energy int // as it joins; each Delta carries it after
	MapID  string
	Sheet  Sheet
	// JoinSeq rises with each join, across restarts too, so the character's later login has the higher one.
	JoinSeq   uint64
	Quests    Quests      // a copy of the log the player joined with
	Timers    SkillTimers // the cooldowns and buffs the player joined with, still running
	Waypoints Waypoints   // a copy of those the player joined having found
}

// PlayerView is a player as everyone on the map sees it.
type PlayerView struct {
	ID kernel.CharacterID
	// Slot is its number on its map, by which the steps that follow a full view of it name it: it keeps it from its join to its leaving.
	Slot  uint32
	Name  string
	Class kernel.Class
	Level int
	HP    int
	MaxHP int
	Dead  bool
	// Invulnerable lasts the hit guard after a mob's hit; Deltas carry its edges.
	Invulnerable bool
	// Dodging lasts a skill's dodge, mobs' attacks passing through; Deltas carry its edges.
	Dodging bool
	// Shield is what's left of its shield buffs' pools, which take mobs' damage before HP.
	Shield   int
	State    sim.State    // Held and Slow carry its statuses' hold on its movement
	Aim      float32      // the yaw its latest attack or skill aimed along
	Statuses []StatusView // its abnormal statuses
	Team     int          // its side in a match, 1 or 2; 0 outside one
	Bot      bool
	Look     Look // its picks; the revision stays in the world
}

// PlayerStep is a player's change when only where it is and how it moves changed since the view or step of it that the frame before had: whole centimetres from its last place, and the fields that differ. X and Z, in metres, file it in a cell.
type PlayerStep struct {
	ID         kernel.CharacterID // which player, for filing it: the wire names it by its slot
	Slot       uint32
	X, Z       float64
	DX, DY, DZ int32
	// What differs from the last, velocity in mm/s and the heading's parts in thousandths; nil when it doesn't.
	VX, VY, VZ, HX, HZ *int32
	OnGround           *bool
	HP, Sprint, Held   *int32
	Aim                *float32
}

// MobStep is a mob's change when only where it is, its yaw, its HP or its pose changed: whole centimetres from its last place, and the fields that differ. X and Z, in metres, file it in a cell.
type MobStep struct {
	ID         uint64
	X, Z       float64
	DX, DY, DZ int32
	Yaw        *int32 // thousandths of a radian
	HP         *int32
	Pose       *MobPose
}

// MobView is a mob as everyone on the map sees it.
type MobView struct {
	ID       uint64
	Type     string
	X, Y, Z  float64 // feet centre, y up
	Yaw      float32 // the way it faces
	HP       int
	MaxHP    int
	Pose     MobPose
	Statuses []StatusView // its abnormal statuses
}

// MobPose is how a mob takes the blows that land on it.
type MobPose int

// The poses: standing, stopped a moment, knocked down, and up again in super armor.
const (
	Standing MobPose = iota
	Flinching
	Down
	Rising
)

// EntityRef names a player or a mob in a combat event.
type EntityRef struct {
	Player kernel.CharacterID // zero when the entity is a mob
	Mob    uint64
}

// IsZero reports whether the reference names nothing, as an unset lock.
func (e EntityRef) IsZero() bool { return e == EntityRef{} }

// IsMob reports whether the reference names a mob.
func (e EntityRef) IsMob() bool { return e.Player.IsZero() }

// Event is something an instance tells a player.
//
//sumtype:decl
type Event interface{ event() }

// Snapshot is what a player sees of the map, the cells round them, sent on join.
type Snapshot struct {
	Tick          uint64
	MapID         string
	Channel       int    // the region's channel, from 1; 0 on any other map
	Area          string // the region's area the joiner stands in; empty on any other map
	Players       []PlayerView
	Mobs          []MobView
	Pouches       []PouchView    // the joiner's own
	FieldBoss     *FieldBoss     // while one is up
	FieldBossGone *FieldBossGone // while the map's is beaten
	Boss          *MobView       // the field boss as it stands while one is up, wherever on the map, for its bar
}

// Frame is what changed on a map in one tick, in parts its players share: each cell's they're sent, and the map's; read-only once sent.
type Frame struct {
	Tick  uint64
	Parts []Part
}

// Part is what changed in one of a map's cells in a tick, or, for the map's own part, its field boss as it changed.
type Part struct {
	Players     []PlayerView
	Steps       []PlayerStep
	Mobs        []MobView
	MobSteps    []MobStep
	Removed     []kernel.CharacterID
	RemovedMobs []uint64
	Boss        *MobView // the map's part: the field boss as it changed, for every player's bar
}

// Recipient is a player a frame goes to: the parts they're sent, by index; AckSeq, the latest input applied for them; their energy as of then when it changed or they just joined; what they stopped seeing; and their own pouches that changed, which nobody else's frame carries.
type Recipient struct {
	Character  kernel.CharacterID
	Connection string
	Parts      []int
	AckSeq     uint32
	Energy     *int
	Unseen     *Unseen     // nil when nothing went out of their sight
	Own        *PlayerView // their own view, whole, when it changed: the client reconciles to it exactly, which a step's rounding wouldn't give
	Pouches    Pouches
}

// Unseen are the bodies a player stopped seeing, out of the cells round them though still on the map.
type Unseen struct {
	Players []kernel.CharacterID
	Mobs    []uint64
}

// Teleport moves the player to another map, whose instance sends the Snapshot.
type Teleport struct {
	Tick    uint64
	MapID   string
	X, Y, Z float64
	Yaw     float32
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
	Dodge  bool   // a mob's attack passing through a player's dodge
	Block  bool   // a mob's attack a player's block stopped
	Status string // a damage-over-time tick's status
	// Shielded is what of Amount a player's shields took.
	Shielded int
}

// Died says a character fell, to everyone on the map.
type Died struct {
	Tick    uint64
	ID      kernel.CharacterID
	X, Y, Z float64
}

// Respawned says a character is back, to the map it left and to the character.
type Respawned struct {
	Tick    uint64
	ID      kernel.CharacterID
	MapID   string
	X, Y, Z float64
	HP      int
	Yaw     float32
}

// ExpGained is EXP awarded to the player, after the award.
type ExpGained struct {
	Tick   uint64
	Amount int64
	Exp    int64
}

// LevelUp carries the player's new level, own stats, sheet with the new points, and restored HP; energy stays as it was.
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
	Sheet Sheet
}

// StatsChanged is the player's HP after what it wears, its points, its regen or a potion changed it.
type StatsChanged struct {
	Tick  uint64
	Sheet *Sheet // set when the sheet changed, as gear and points change it; nil when only HP moved
	HP    int
	MaxHP int
	// Timers, when set, replace every cooldown and buff the player holds: entering and leaving a match, and at each round's reset.
	Timers *SkillTimers
}

func (Snapshot) event() {}

func (StatsChanged) event() {}

func (Teleport) event() {}
func (Detached) event() {}

// TeleportCancelled tells the player their teleport channel ended before it took them, and why.
type TeleportCancelled struct {
	Tick   uint64
	Reason TeleportCancel
}

// TeleportCancel is what ended a teleport channel early.
type TeleportCancel int

// The causes: a movement or jump key, an attack or skill starting, a mob's landed hit, and death.
const (
	TeleportMoved TeleportCancel = iota + 1
	TeleportActed
	TeleportHit
	TeleportDied
)

func (TeleportCancelled) event() {}

func (Damage) event() {}

func (Died) event() {}

func (Respawned) event() {}

func (ExpGained) event() {}

func (LevelUp) event() {}

// SkillUsed says a character used a skill at a stage, aimed along a yaw, to everyone on the map, for drawing; its damage goes out as Damage.
type SkillUsed struct {
	Tick   uint64
	Caster kernel.CharacterID
	Skill  string
	Stage  int
	Trait  string // the extra trait it was used with, "" for the default
	Aim    float32
	Target EntityRef // the lock it was aimed at, zero for none
}

func (SkillUsed) event() {}

// AttackUsed says a character's basic attack started, aimed along a yaw, lasting Length ticks, the class's length, to everyone on the map, for drawing; its damage goes out as Damage.
type AttackUsed struct {
	Tick     uint64
	Attacker kernel.CharacterID
	Length   uint64
	Aim      float32
}

func (AttackUsed) event() {}

// MobAttacked says a mob's attack began its windup, an index into its type's attacks, turned to a yaw, to everyone on the map, for drawing; its damage goes out as Damage.
type MobAttacked struct {
	Tick          uint64
	Mob           uint64
	Attack        int
	Yaw           float32
	ToX, ToY, ToZ float64 // where its hit goes: a leap's landing or a rush's end; for other kinds, where the mob stands
}

func (MobAttacked) event() {}

// Checkpoint is a character's non-economic state, saved every interval and at once on a level-up, map change or leave.
type Checkpoint struct {
	ID        kernel.CharacterID
	Class     kernel.Class // a promotion changes it
	MapID     string       // the map it is on, or moving to
	X, Y, Z   float64
	Yaw       float32
	HP        int
	Energy    int
	Level     int
	Exp       int64
	Points    Points
	Skills    Skills // a copy of the player's
	Quests    Quests // the whole log, a copy of the player's
	Timers    SkillTimers
	Waypoints Waypoints // every one found, a copy of the player's
}

// InventoryChanged tells the player which slots and equip slots a committed change touched, as they stood at the inventory's Revision.
type InventoryChanged struct {
	Tick      uint64
	Revision  uint64
	Slots     []Slot
	Equipment []EquipSlot
}

// Inventory is slots of a character's inventory as they stood at Revision, the item module's count of its changes.
type Inventory struct {
	Revision uint64
	Slots    []Slot
}

// Slot is one inventory slot a change touched; Item is nil when it emptied.
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

// EquipCall wears an inventory item, checked against the character's live level, class and own stats, without gear; Key is the client's request ID.
type EquipCall struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
	Key       string
	Item      kernel.ItemID
	EquipSlot string // rings only
	Level     int
	Class     kernel.Class
	Stats     gamedata.Stats
}

// UseCall takes one of a usable item from its stack; Template is what the client saw there, and the item module refuses any other item.
type UseCall struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
	Key       string
	Item      kernel.ItemID
	Template  string
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

func (InventoryChanged) event() {}
