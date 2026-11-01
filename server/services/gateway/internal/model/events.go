package model

import (
	"finga/internal/kernel"
	"finga/internal/protocol"
)

// Joined is a player the world has joined, and where it is.
type Joined struct {
	Character protocol.CharacterView
	MapID     string
	X, Y      float64
}

// WorldEvent is Subscribed (the stream opened, perhaps after a break) or a delivery for one player.
type WorldEvent struct {
	Subscribed bool
	Character  kernel.CharacterID
	Delivery   Delivery
}

// SocialEvent is Subscribed with the service's boot ID, or a delivery for one player.
type SocialEvent struct {
	Subscribed bool
	BootID     string
	Character  kernel.CharacterID
	Delivery   Delivery
}

// SessionEnd ends one login session (Session set) or all of an account's (Account set).
type SessionEnd struct {
	Session  kernel.SessionID
	Account  kernel.AccountID
	NewLogin bool
}

// ChatLine is a chat line the social service accepted and cleaned.
type ChatLine struct {
	Channel protocol.ChatChannel
	Text    string
}
