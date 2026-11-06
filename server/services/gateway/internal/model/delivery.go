package model

// Delivery is a push for one player at a tick; the use cases route it, hold it and shift its Tick.
type Delivery struct {
	Tick uint64
	Push Push
}

// Push is what a service tells a player; the WebSocket handler turns each into its message.
//
//sumtype:decl
type Push interface{ push() }

// Snapshot is everything on the player's map; a join waits for it.
type Snapshot struct {
	Players []PlayerView
	Mobs    []MobView
	Drops   []DropView
}

// Delta is what changed on the player's map since the last tick, and AckSeq, the player's last input the world applied.
type Delta struct {
	*Frame
	AckSeq uint32
}

// Frame is what changed on a map in one tick; it is shared, and read-only.
type Frame struct {
	Players                   []PlayerView
	Mobs                      []MobView
	Drops                     []DropView
	RemovedPlayers            []string
	RemovedMobs, RemovedDrops []uint64
}

// MapChange moves the player to another map.
type MapChange struct {
	MapID string
	X, Y  float64
}

// Damage is a hit.
type Damage struct {
	Source, Target Entity
	Amount         int
	Crit, Miss     bool
}

// Died is a player dying where it stood.
type Died struct {
	CharacterID string
	X, Y        float64
}

// Respawned is a player back on its feet.
type Respawned struct {
	CharacterID string
	MapID       string
	X, Y        float64
	HP          int
}

// ExpGained is experience earned, and the total.
type ExpGained struct {
	Amount, Exp int64
}

// LevelUp is the player's character after it levelled up.
type LevelUp struct {
	Character CharacterView
}

// ChatMessage is a map or party line.
type ChatMessage struct {
	Channel            ChatChannel
	Sender, SenderName string
	Text               string
}

// InventoryChanged is the slots a pickup or another move changed.
type InventoryChanged struct {
	Slots []InventorySlot
}

// PickupFailed is a pickup the item service refused, as the player sees it.
type PickupFailed struct {
	Why Public
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

func (Snapshot) push()         {}
func (Delta) push()            {}
func (MapChange) push()        {}
func (Damage) push()           {}
func (Died) push()             {}
func (Respawned) push()        {}
func (ExpGained) push()        {}
func (LevelUp) push()          {}
func (ChatMessage) push()      {}
func (InventoryChanged) push() {}
func (PickupFailed) push()     {}
func (PartyState) push()       {}
func (PartyInvited) push()     {}
