// Package protocol is the Go side of the WebSocket message catalogue
// (Low-level design, page 4; api/asyncapi.yaml): the Stage 1 message
// structs, registered with pkg/wsconn's codec. Only the gateway imports
// it; application modules return their own result types and the gateway
// maps them to these.
package protocol

import "finga/pkg/wsconn"

// Version is the protocol version Hello must carry. It changes when a
// message changes shape; an old client is kicked with KickOutdatedProtocol.
const Version = 1

// KickReason says why the server is about to close a connection.
type KickReason string

// The kick reasons (page 4).
const (
	KickLoggedInElsewhere KickReason = "logged_in_elsewhere"
	KickOutdatedProtocol  KickReason = "outdated_protocol"
	KickServerRestart     KickReason = "server_restart"
	KickAbuse             KickReason = "abuse"
	KickUnauthenticated   KickReason = "unauthenticated"
	KickSessionExpired    KickReason = "session_expired" // the session ended: expired, logged out, or its token was reused
)

// ChatChannel is where a chat message goes.
type ChatChannel string

// The chat channels.
const (
	ChatMap   ChatChannel = "map"
	ChatParty ChatChannel = "party"
)

// --- Client to server ---

// Hello is the first message, within 5 seconds of connecting.
type Hello struct {
	Token           string `json:"token"`
	ProtocolVersion int    `json:"protocol_version"`
	CharacterID     string `json:"character_id"`
}

// Input is the held keys; the sequence number travels in the envelope.
type Input struct {
	Left  bool `json:"left,omitempty"`
	Right bool `json:"right,omitempty"`
	Up    bool `json:"up,omitempty"`
	Down  bool `json:"down,omitempty"`
	Jump  bool `json:"jump,omitempty"`
}

// Attack swings or shoots the way the character faces (seq in the envelope).
type Attack struct{}

// PickUp takes the nearest eligible drop in range.
type PickUp struct{}

// EnterPortal uses the portal the character stands in.
type EnterPortal struct{}

// ChatSend is a chat line, up to 120 characters.
type ChatSend struct {
	Channel ChatChannel `json:"channel"`
	Text    string      `json:"text"`
}

// PartyInvite invites a character on the same map.
type PartyInvite struct {
	CharacterID string `json:"character_id"`
}

// PartyRespond accepts or declines an invite.
type PartyRespond struct {
	InviteID string `json:"invite_id"`
	Accept   bool   `json:"accept"`
}

// PartyLeave leaves the party.
type PartyLeave struct{}

// PartyKick removes a member (leader only).
type PartyKick struct {
	CharacterID string `json:"character_id"`
}

// PartyPromote hands leadership over (leader only).
type PartyPromote struct {
	CharacterID string `json:"character_id"`
}

// InventoryMove moves an item to a slot (req and idempotency key).
type InventoryMove struct {
	IdempotencyKey string `json:"idempotency_key"`
	ItemID         string `json:"item_id"`
	Tab            string `json:"tab"`
	Slot           int    `json:"slot"`
}

// InventoryDiscard destroys some or all of an item.
type InventoryDiscard struct {
	IdempotencyKey string `json:"idempotency_key"`
	ItemID         string `json:"item_id"`
	Quantity       int    `json:"quantity"`
}

// Ping measures latency; Pong echoes the client time.
type Ping struct {
	ClientTime int64 `json:"client_time"` // Unix milliseconds
}

// --- Server to client ---

// Stats are the five primary stats.
type Stats struct {
	STR int `json:"str"`
	DEX int `json:"dex"`
	INT int `json:"int"`
	AGI int `json:"agi"`
	VIT int `json:"vit"`
}

// CharacterView is the player's own character, in Welcome and LevelUp.
type CharacterView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Class string `json:"class"`
	Level int    `json:"level"`
	Exp   int64  `json:"exp"`
	Stats Stats  `json:"stats"`
	HP    int    `json:"hp"`
	MaxHP int    `json:"max_hp"`
	MP    int    `json:"mp"`
	MaxMP int    `json:"max_mp"`
}

// ItemView is one inventory slot.
type ItemView struct {
	ItemID   string `json:"item_id"`
	Template string `json:"template"`
	Tab      string `json:"tab"`
	Slot     int    `json:"slot"`
	Quantity int    `json:"quantity"`
}

// PartyMember is one member as others see it.
type PartyMember struct {
	CharacterID string `json:"character_id"`
	Name        string `json:"name"`
	Level       int    `json:"level"`
	HP          int    `json:"hp"`
	MaxHP       int    `json:"max_hp"`
	MapID       string `json:"map_id"`
	Online      bool   `json:"online"`
}

// PartyView is the party, or absent.
type PartyView struct {
	Leader  string        `json:"leader"`
	Members []PartyMember `json:"members"`
}

// Welcome answers Hello. The wallet joins in Stage 2.
type Welcome struct {
	Token      string        `json:"token"` // the rotated session token; the client uses it from now on (step 16)
	Character  CharacterView `json:"character"`
	Inventory  []ItemView    `json:"inventory"`
	Party      *PartyView    `json:"party,omitempty"`
	MapID      string        `json:"map_id"`
	Tick       uint64        `json:"tick"`
	ServerTime int64         `json:"server_time"` // Unix milliseconds
}

// PlayerView is a character on the map as everyone sees it.
type PlayerView struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Class    string  `json:"class"`
	Level    int     `json:"level"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	VX       float64 `json:"vx"`
	VY       float64 `json:"vy"`
	Facing   int     `json:"facing"`
	OnGround bool    `json:"on_ground"`
	Climbing bool    `json:"climbing"`
	HP       int     `json:"hp"`
	MaxHP    int     `json:"max_hp"`
	Dead     bool    `json:"dead,omitempty"`
	// Invulnerable is set while the second after contact damage lasts (step 19).
	Invulnerable bool `json:"invulnerable,omitempty"`
}

// MobView is a mob on the map.
type MobView struct {
	ID     uint64  `json:"id"`
	Type   string  `json:"type"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Facing int     `json:"facing"`
	HP     int     `json:"hp"`
	MaxHP  int     `json:"max_hp"`
}

// DropView is an item on the ground.
type DropView struct {
	ID       uint64  `json:"id"`
	Template string  `json:"template"`
	Quantity int     `json:"quantity"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
}

// Snapshot is everything on the map, on join and map change.
type Snapshot struct {
	Players []PlayerView `json:"players"`
	Mobs    []MobView    `json:"mobs"`
	Drops   []DropView   `json:"drops"`
}

// Delta is what changed this tick (tick in the envelope). Lists hold the
// changed or new entities; the removed lists hold IDs that left.
type Delta struct {
	AckSeq         uint32       `json:"ack_seq"`
	Players        []PlayerView `json:"players,omitempty"`
	Mobs           []MobView    `json:"mobs,omitempty"`
	Drops          []DropView   `json:"drops,omitempty"`
	RemovedPlayers []string     `json:"removed_players,omitempty"`
	RemovedMobs    []uint64     `json:"removed_mobs,omitempty"`
	RemovedDrops   []uint64     `json:"removed_drops,omitempty"`
}

// Correction tells the client its prediction diverged at seq.
type Correction struct {
	Seq      uint32  `json:"seq"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	VX       float64 `json:"vx"`
	VY       float64 `json:"vy"`
	OnGround bool    `json:"on_ground"`
	Climbing bool    `json:"climbing"`
}

// Damage is one resolved hit. Source and target are player IDs or mob IDs
// as strings ("p:<uuid>" or "m:<id>").
type Damage struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Amount int    `json:"amount"`
	Crit   bool   `json:"crit,omitempty"`
	Miss   bool   `json:"miss,omitempty"`
}

// Died says a character fell.
type Died struct {
	CharacterID string  `json:"character_id"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
}

// Respawned says a character is back.
type Respawned struct {
	CharacterID string  `json:"character_id"`
	MapID       string  `json:"map_id"`
	X           float64 `json:"x"`
	Y           float64 `json:"y"`
	HP          int     `json:"hp"`
}

// ExpGained is EXP awarded to the player.
type ExpGained struct {
	Amount int64 `json:"amount"`
	Exp    int64 `json:"exp"` // within the level, after the award
}

// LevelUp carries the new level and stats.
type LevelUp struct {
	Character CharacterView `json:"character"`
}

// InventoryChanged lists the slots that changed; an empty slot has no item.
type InventoryChanged struct {
	Slots []InventorySlot `json:"slots"`
}

// InventorySlot is a slot after a change: Item is nil when it emptied.
type InventorySlot struct {
	Tab  string    `json:"tab"`
	Slot int       `json:"slot"`
	Item *ItemView `json:"item,omitempty"`
}

// MapChange follows a portal.
type MapChange struct {
	MapID string  `json:"map_id"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

// ChatMessage is a line from another player (or the sender's own echo).
type ChatMessage struct {
	Channel    ChatChannel `json:"channel"`
	Sender     string      `json:"sender"`
	SenderName string      `json:"sender_name"`
	Text       string      `json:"text"`
}

// PartyState is the whole party after any change; Party nil means none.
type PartyState struct {
	Party *PartyView `json:"party"`
}

// PartyInvited is an invite to the recipient.
type PartyInvited struct {
	InviteID string `json:"invite_id"`
	From     string `json:"from"`
	FromName string `json:"from_name"`
}

// Response answers a command that carried a request ID (req in the envelope).
type Response struct {
	OK      bool   `json:"ok"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// Kicked precedes a server-side close.
type Kicked struct {
	Reason KickReason `json:"reason"`
}

// Pong answers Ping.
type Pong struct {
	ClientTime int64 `json:"client_time"`
	ServerTime int64 `json:"server_time"`
}

// NewCodec is the JSON codec with every Stage 1 message registered.
func NewCodec() *wsconn.JSONCodec {
	c := wsconn.NewJSONCodec()
	for name, zero := range catalogue() {
		c.Register(name, zero)
	}
	return c
}

// catalogue maps names to zero values; iteration order does not matter
// because Register is order-independent.
func catalogue() map[string]any {
	return map[string]any{
		"Hello": Hello{}, "Input": Input{}, "Attack": Attack{}, "PickUp": PickUp{}, "EnterPortal": EnterPortal{},
		"ChatSend": ChatSend{}, "PartyInvite": PartyInvite{}, "PartyRespond": PartyRespond{}, "PartyLeave": PartyLeave{},
		"PartyKick": PartyKick{}, "PartyPromote": PartyPromote{}, "InventoryMove": InventoryMove{},
		"InventoryDiscard": InventoryDiscard{}, "Ping": Ping{},
		"Welcome": Welcome{}, "Snapshot": Snapshot{}, "Delta": Delta{}, "Correction": Correction{}, "Damage": Damage{},
		"Died": Died{}, "Respawned": Respawned{}, "ExpGained": ExpGained{}, "LevelUp": LevelUp{},
		"InventoryChanged": InventoryChanged{}, "MapChange": MapChange{}, "ChatMessage": ChatMessage{},
		"PartyState": PartyState{}, "PartyInvited": PartyInvited{}, "Response": Response{}, "Kicked": Kicked{}, "Pong": Pong{},
	}
}
