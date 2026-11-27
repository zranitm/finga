package account

import (
	"time"

	"finga/internal/shared/kernel"
)

// Player is the character a connection plays, as its party sees it.
type Player struct {
	ID    kernel.CharacterID
	Name  string
	Class string
	Level int
	HP    int
	MaxHP int
}

// Presence is a connected player: its character and the connection it joined through; every world command carries it.
type Presence struct {
	Character  kernel.CharacterID
	Connection string
}

// Actor is who acts on items: the account scopes idempotency keys, the character owns the items.
type Actor struct {
	Account   kernel.AccountID
	Character kernel.CharacterID
}

// EndReason is why the gateway ends a player's session; the handler decides what the client sees.
type EndReason string

// The reasons a session ends.
const (
	EndLoggedInElsewhere EndReason = "logged_in_elsewhere"
	EndSessionExpired    EndReason = "session_expired"
	EndServerRestart     EndReason = "server_restart"
	EndDeliveryBacklog   EndReason = "delivery_backlog" // a join held more deliveries than it may
	EndRejoinFailed      EndReason = "rejoin_failed"
	EndTickRateChanged   EndReason = "tick_rate_changed" // a rejoin found the world at another rate, which the client learns from a new Welcome
)

// Joined is a player the world has joined, and on which map: what the connection flows need of the join, and the world's own view of it, which the world's handler reads for the Welcome.
type Joined struct {
	Player     Player
	MapID      string
	TickRateHz int    // the world's ticks per second
	JoinSeq    uint64 // rises with each join, so the character's later login has the higher one
	Waypoints  []string
	View       any // the world's own view of the character, as it joined
}

// Welcome is what a joined player's screen starts from, after a login or a rejoin the player didn't ask for. Each module's handler fills its part from what the module sent, which the connection flows hold without reading.
type Welcome struct {
	Token      string // replaces the one Hello sent; empty after a rejoin, where the client keeps its own
	Player     Player
	MapID      string
	ServerTime time.Time
	TickRateHz int
	Waypoints  []string // the ids of those found
	Joined     any      // the world's view of the character, as it joined
	Inventory  any      // the item module's inventory
	Missed     any      // the pvp module's matches that ended while the player was away
	Written    func()   // run once the Welcome is written to the socket, never if it isn't; nil for none
}

// MissedMatches are the matches the Welcome tells of, which are marked told once it is written.
type MissedMatches struct {
	Told    []kernel.MatchID
	Details any // the pvp module's own account of them
}
