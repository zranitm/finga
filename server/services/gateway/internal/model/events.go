package model

import "finga/internal/kernel"

// Joined is a player the world has joined, and where it is.
type Joined struct {
	Character  CharacterView
	MapID      string
	X, Y       float64
	TickRateHz int    // the world's ticks per second
	JoinSeq    uint64 // rises with each join, so the character's later login has the higher one
	Quests     []QuestState
	Timers     []SkillTimer // the cooldowns and buffs still running, which a fresh join keeps
}

// WorldEvent is what the world's delivery stream brings.
//
//sumtype:decl
type WorldEvent interface{ worldEvent() }

// WorldSubscribed is the stream opening, perhaps after a break.
type WorldSubscribed struct{}

// WorldDelivery is a push for the player joined through Connection.
type WorldDelivery struct {
	Connection string
	Character  kernel.CharacterID
	Delivery   Delivery
}

// Detached is the world having the character on no map through Connection any more; it must join again.
type Detached struct {
	Connection string
	Character  kernel.CharacterID
}

func (WorldSubscribed) worldEvent() {}
func (WorldDelivery) worldEvent()   {}
func (Detached) worldEvent()        {}

// SocialEvent is what the social service's delivery stream brings.
//
//sumtype:decl
type SocialEvent interface{ socialEvent() }

// SocialSubscribed is the stream opening; a new BootID means the service restarted and every party is gone.
type SocialSubscribed struct{ BootID string }

// SocialDelivery is a push for one player.
type SocialDelivery struct {
	Character kernel.CharacterID
	Delivery  Delivery
}

func (SocialSubscribed) socialEvent() {}
func (SocialDelivery) socialEvent()   {}

// SessionEnd is what the account service's session stream brings.
//
//sumtype:decl
type SessionEnd interface{ sessionEnd() }

// SessionRevoked ends one login session.
type SessionRevoked struct{ Session kernel.SessionID }

// AccountRevoked ends all of an account's login sessions; NewLogin when a login elsewhere did it.
type AccountRevoked struct {
	Account  kernel.AccountID
	NewLogin bool
}

func (SessionRevoked) sessionEnd() {}
func (AccountRevoked) sessionEnd() {}

// ChatLine is a chat line the social service accepted and cleaned.
type ChatLine struct {
	Channel ChatChannel
	Text    string
}
