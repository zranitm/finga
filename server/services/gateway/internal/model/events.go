package model

import "finga/internal/kernel"

// Joined is a player the world has joined, and where it is.
type Joined struct {
	Character CharacterView
	MapID     string
	X, Y      float64
}

// WorldEvent is Subscribed (the stream opened, perhaps after a break) or a delivery for the player
// joined through one connection.
type WorldEvent struct {
	Subscribed bool
	Connection string
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
	Channel ChatChannel
	Text    string
}
