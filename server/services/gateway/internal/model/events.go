package model

import (
	"finga/internal/kernel"
	"finga/internal/protocol"
)

// Joined is a player the world has joined: the character as the player
// sees it, and where it is.
type Joined struct {
	Character protocol.CharacterView
	MapID     string
	X, Y      float64
}

// WorldEvent is one message of the world's delivery stream: Subscribed
// (the stream is open, again after a break) or a message for one player.
type WorldEvent struct {
	Subscribed bool
	Character  kernel.CharacterID
	Message    Message
}

// SocialEvent is one message of the social service's stream: Subscribed
// with the service's boot ID (a new one means it restarted and forgot
// every party), or a message for one player.
type SocialEvent struct {
	Subscribed bool
	BootID     string
	Character  kernel.CharacterID
	Message    Message
}

// SessionEnd is one message of the account service's stream: a login
// session ended (Session set), or every session of an account did
// (Account set; NewLogin when a login elsewhere ended them).
type SessionEnd struct {
	Session  kernel.SessionID
	Account  kernel.AccountID
	NewLogin bool
}

// ChatLine is a chat line the social service accepted: its channel and its
// cleaned text.
type ChatLine struct {
	Channel protocol.ChatChannel
	Text    string
}
