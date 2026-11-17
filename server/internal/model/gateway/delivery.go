package gateway

// Delivery is a push for one player at a tick; the use cases route it, hold it and shift its Tick.
type Delivery struct {
	Tick uint64
	Push Push
}

// Push is what a module tells a player; the WebSocket handler turns each into its message.
//
//sumtype:decl
type Push interface{ push() }

// Snapshot is everything on the player's map; a join waits for it.
type Snapshot struct {
	Players   []PlayerView
	Mobs      []MobView
	Drops     []DropView
	FieldBoss *FieldBoss // while one is up
}

// FieldBoss is the map's field boss: its mob, its type, and how long until it leaves.
type FieldBoss struct {
	MobID      uint64
	Type       string
	LeavesInMs int64
}

// FieldBossGone is the map's field boss beaten, or gone unbeaten.
type FieldBossGone struct {
	MobID  uint64
	Beaten bool
}

// BossAppeared is the news, for every player online, of a field boss appearing on a map.
type BossAppeared struct {
	Boss  string
	MapID string
}

// Delta is what changed on the player's map since the last tick, AckSeq, the player's last input the world applied, and the player's energy as of then when it changed or the player just joined.
type Delta struct {
	*Frame
	AckSeq uint32
	Energy *uint32
}

// Frame is what changed on a map in one tick; it is shared, and read-only.
type Frame struct {
	Players                   []PlayerView
	Mobs                      []MobView
	Drops                     []DropView
	RemovedPlayers            []string
	RemovedMobs, RemovedDrops []uint64
	CollectedDrops            []uint64 // left the ground for their owners' inventories
}

// MapChange moves the player to another map.
type MapChange struct {
	MapID   string
	X, Y, Z float64
	Yaw     float32
}

// TeleportCancelled says the player's teleport channel ended before it took them, and why.
type TeleportCancelled struct {
	Reason TeleportCancel
}

// TeleportCancel is what ended a teleport channel.
type TeleportCancel int

// The causes: unknown, a movement or jump key, an attack or skill starting, a mob's landed hit, and death.
const (
	TeleportCancelUnknown TeleportCancel = iota
	TeleportMoved
	TeleportActed
	TeleportHit
	TeleportDied
)

// Damage is a hit.
type Damage struct {
	Source, Target Entity
	Amount         int
	Crit, Miss     bool
	Dodge          bool   // a mob's attack passing through a player's dodge
	Status         string // a damage-over-time tick's status
}

// SkillUsed is a player using a skill at a stage, an index into its stages in game data.
type SkillUsed struct {
	Caster string
	Skill  string
	Stage  int
	Aim    float32 // the yaw it aimed along
}

// AttackUsed is a player's basic attack starting, lasting LengthTicks, the class's length.
type AttackUsed struct {
	Attacker    string
	LengthTicks int
	Aim         float32 // the yaw it aimed along
}

// MobAttacked is a mob's attack beginning its windup, an index into its type's attacks in game data.
type MobAttacked struct {
	MobID         uint64
	Attack        int
	Yaw           float32
	ToX, ToY, ToZ float64 // where its hit goes: a leap's landing or a rush's end; for other kinds, where the mob stands
}

// Died is a player dying where it stood.
type Died struct {
	CharacterID string
	X, Y, Z     float64
}

// Respawned is a player back on its feet.
type Respawned struct {
	CharacterID string
	MapID       string
	X, Y, Z     float64
	HP          int
	Yaw         float32
}

// ExpGained is experience earned, and the total.
type ExpGained struct {
	Amount, Exp int64
}

// LevelUp is the player's character after it levelled up.
type LevelUp struct {
	Character CharacterView
}

// Promoted is the player's character in its new class.
type Promoted struct {
	Character CharacterView
}

// ChatMessage is a map or party line.
type ChatMessage struct {
	Channel            ChatChannel
	Sender, SenderName string
	Text               string
}

// InventoryChanged is the slots and equip slots a grant, a move or an equip changed, at the inventory's revision after it.
type InventoryChanged struct {
	Revision  uint64
	Slots     []InventorySlot
	Equipment []EquipSlot
}

// StatsChanged is the player's HP after what it wears, its points, its regen or a potion changed it.
type StatsChanged struct {
	Sheet     *StatSheet // set when the sheet changed; nil when only HP moved, and the client keeps its last
	HP, MaxHP int
}

// LootKind is what became of a drop.
type LootKind int

// What became of a drop.
const (
	LootCollected LootKind = iota + 1
	LootWaiting
	LootLost
)

// Loot is what became of a drop: the player's own, or a party member's they saw collected.
type Loot struct {
	Kind             LootKind
	MapID            string
	DropID           uint64
	Owner, OwnerName string
	Template         string
	Quantity         int
}

// PartyState is the player's party; nil means none.
type PartyState struct {
	Party *PartyView
}

// PartyInvited is an invite to the sender's party.
type PartyInvited struct {
	InviteID       string
	From, FromName string
}

func (Snapshot) push()          {}
func (Delta) push()             {}
func (MapChange) push()         {}
func (TeleportCancelled) push() {}
func (Damage) push()            {}
func (Died) push()              {}
func (SkillUsed) push()         {}
func (AttackUsed) push()        {}
func (MobAttacked) push()       {}
func (Respawned) push()         {}
func (ExpGained) push()         {}
func (LevelUp) push()           {}
func (Promoted) push()          {}
func (ChatMessage) push()       {}
func (InventoryChanged) push()  {}
func (StatsChanged) push()      {}
func (Loot) push()              {}
func (PartyState) push()        {}
func (PartyInvited) push()      {}
func (FieldBoss) push()         {}
func (FieldBossGone) push()     {}
func (BossAppeared) push()      {}
