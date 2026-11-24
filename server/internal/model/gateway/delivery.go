package gateway

import "time"

// Delivery is a push for one player at a tick; the use cases route it, hold it and shift its Tick.
type Delivery struct {
	Tick uint64
	Push Push
}

// Push is what a module tells a player; the WebSocket handler turns each into its message.
//
//sumtype:decl
type Push interface{ push() }

// Snapshot is what the player sees of their map, the cells round them; a join waits for it.
type Snapshot struct {
	Channel       int    // the region's channel, from 1; 0 on any other map
	Area          string // the region's area the player stands in; empty on any other map
	Players       []PlayerView
	Mobs          []MobView
	Pouches       []PouchView    // the player's own
	FieldBoss     *FieldBoss     // while one is up
	FieldBossGone *FieldBossGone // while the map's is beaten
	Boss          *MobView       // the field boss as it stands while one is up, wherever on the map, for its bar
}

// FieldBoss is the map's field boss: its mob and its type. It stays until it's beaten.
type FieldBoss struct {
	MobID uint64
	Type  string
}

// FieldBossGone is the map's field boss beaten: the mob it was, its type, and how long until it's back.
type FieldBossGone struct {
	MobID    uint64
	Type     string
	BackInMs int64
}

// Delta is what changed round the player since the last tick: the frame's Parts the player is sent, by index; AckSeq, the player's last input the world applied; the player's energy as of then when it changed or the player just joined; what the player stopped seeing; and the player's own pouches that changed, which no other player's Delta carries.
type Delta struct {
	Frame       *Frame
	Parts       []int
	AckSeq      uint32
	Energy      *uint32
	Unseen      *Unseen // nil when nothing went out of their sight
	Pouches     []PouchView
	PouchesGone []uint64
}

// Frame is what changed on a map in one tick, in parts its players share; read-only.
type Frame struct {
	Parts []Part
}

// Part is what changed in one of a map's cells in a tick, or, for the map's own part, its field boss as it changed.
type Part struct {
	Players        []PlayerView
	Mobs           []MobView
	RemovedPlayers []string
	RemovedMobs    []uint64
	Boss           *MobView // the map's part: the field boss as it changed, for every player's bar
}

// Unseen are the bodies a player stopped seeing, out of the cells round them though still on the map.
type Unseen struct {
	Players []string
	Mobs    []uint64
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

// PartySpots is where the player's party members on their map stand that the frames don't bring; a member it doesn't list isn't here.
type PartySpots struct {
	Spots []PartySpot
}

// PartySpot is a party member where they stand.
type PartySpot struct {
	CharacterID string
	X, Z        float64
}

// AreaEntered says the player stepped into another of the region's areas, by its id, and of its field boss.
type AreaEntered struct {
	Area          string
	FieldBoss     *FieldBoss     // while the area's is up
	FieldBossGone *FieldBossGone // while the area's is beaten
}

// WaypointFound says the player found a waypoint, by its id across every region.
type WaypointFound struct {
	Waypoint string
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
	Block          bool   // a mob's attack a player's block stopped
	Status         string // a damage-over-time tick's status
	Shielded       int    // what of Amount a player's shields took
}

// SkillUsed is a player using a skill at a stage, an index into its stages in game data, with an extra trait or its default.
type SkillUsed struct {
	Caster string
	Skill  string
	Stage  int
	Trait  string  // "" for the default
	Aim    float32 // the yaw it aimed along
	Target Entity  // the lock it was aimed at, zero for none
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
	Retimed   bool         // Timers replace every cooldown and buff the client holds
	Timers    []SkillTimer // each with its time left as the event went out
}

// ArenaBar is the bar a match puts on its player at a countdown: from then, their skills go from its keys alone.
type ArenaBar struct {
	Bar []string
}

// MatchState is a match as its fighters see it, on entry and at each change.
type MatchState struct {
	Match  string
	Mode   string
	Phase  MatchPhase
	Round  int
	Wins   [2]int
	EndsIn time.Duration
	Sides  [2][]MatchFighter
	Battle bool // fought for points: Score and Points are its, and it has no rounds
	Score  [2]int
	Points []CapturePoint
}

// CapturePoint is a battle's point: its holder, 0 for none, and its progress from -1, side 1's, to 1, side 2's, held still while contested.
type CapturePoint struct {
	ID        string
	Holder    int
	Progress  float64
	Contested bool
}

// MatchPhase is where a match stands.
type MatchPhase int

// The phases: before the first round or the battle, counting into a round, fighting, between rounds, and the outcome. A battle has no count or rounds.
const (
	PhasePrep MatchPhase = iota
	PhaseCountdown
	PhaseFight
	PhaseBetween
	PhaseResult
)

// MatchFighter is a fighter as the match lists them; a bot's id is its own.
type MatchFighter struct {
	ID, Name, Class string
	Bot             bool
}

// LootKind is what became of a pouch's items.
type LootKind int

// What became of a pouch's items.
const (
	LootTaken LootKind = iota + 1
	LootLeft
)

// Loot is what became of a pouch's items: the player's own taken or left, or a party member's taken.
type Loot struct {
	Kind             LootKind
	MapID            string
	Pouch            uint64
	Owner, OwnerName string
	Items            []LootItem
}

// LootItem is an item a Loot names.
type LootItem struct {
	Template string
	Quantity int
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
func (PartySpots) push()        {}
func (AreaEntered) push()       {}
func (ArenaBar) push()          {}
func (WaypointFound) push()     {}
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

// PvpQueued says the player is in a mode's queue, having waited Since, and whether their match waits for room for its bots.
type PvpQueued struct {
	Mode        string
	Since       time.Duration
	Members     []string
	WaitingRoom bool
}

// PvpUnqueued says the player left the queue without a match; By is the members whose answer took the ticket out.
type PvpUnqueued struct {
	Reason PvpUnqueueReason
	By     []string
}

// PvpUnqueueReason is why.
type PvpUnqueueReason int

// The reasons.
const (
	PvpUnqueueLeft PvpUnqueueReason = iota
	PvpUnqueueDeclined
	PvpUnqueueLapsed
	PvpUnqueuePartyChanged
)

// PvpOffer is a match found, with how many bots and how long it stands.
type PvpOffer struct {
	Offer, Mode string
	Bots        int
	Until       time.Duration
}

// PvpOfferCancelled says an offer fell through, and whether the player is back in the queue.
type PvpOfferCancelled struct {
	Offer    string
	Reason   PvpGoneReason
	Requeued bool
}

// PvpGoneReason is why an offer fell through.
type PvpGoneReason int

// The reasons.
const (
	PvpGoneDeclined PvpGoneReason = iota
	PvpGoneLapsed
)

// MatchEnded is a match's outcome for the player, with their rating's change in a rated mode.
type MatchEnded struct {
	Mode   string
	Result MatchOutcome
	Battle bool // fought for points: Score is its, and it has no rounds
	Wins   [2]int
	Score  [2]int
	Rating *PvpRatingChange
	Left   bool
}

// MatchOutcome is a match's end for the player's side.
type MatchOutcome int

// The outcomes.
const (
	MatchWin MatchOutcome = iota
	MatchLoss
	MatchDraw
)

// PvpRatingChange is the shown rating and rank before and after, and the placement matches left.
type PvpRatingChange struct {
	Before, After         int
	RankBefore, RankAfter string
	PlacementLeft         int
}

func (StatsChanged) push()      {}
func (PvpQueued) push()         {}
func (PvpUnqueued) push()       {}
func (PvpOffer) push()          {}
func (PvpOfferCancelled) push() {}
func (MatchEnded) push()        {}
func (MatchState) push()        {}
func (Loot) push()              {}
func (PartyState) push()        {}
func (PartyInvited) push()      {}
func (FieldBoss) push()         {}
func (FieldBossGone) push()     {}
