package gateway

import "finga/internal/shared/kernel"

// Joined is a player the world has joined, and on which map.
type Joined struct {
	Character  CharacterView
	MapID      string
	TickRateHz int    // the world's ticks per second
	JoinSeq    uint64 // rises with each join, so the character's later login has the higher one
	Quests     []QuestState
	Timers     []SkillTimer // the cooldowns and buffs still running, which a fresh join keeps
	Waypoints  []string     // the ids of those found
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

// SocialEvent is what the social module's delivery stream brings.
//
//sumtype:decl
type SocialEvent interface{ socialEvent() }

// SocialSubscribed is the stream opening, first or again after a break: parties outlive a break, in one process.
type SocialSubscribed struct{}

// SocialDelivery is a push for one player.
type SocialDelivery struct {
	Character kernel.CharacterID
	Delivery  Delivery
}

// PvpDelivery is a pvp push for one player, which the gateway hands on if it has them.
type PvpDelivery struct {
	Character kernel.CharacterID
	Delivery  Delivery
	Untold    kernel.MatchID // a match the player was away from at its end: handed on, it's marked told; zero for any other push
}

func (SocialSubscribed) socialEvent() {}
func (SocialDelivery) socialEvent()   {}

// SessionEnd is what the account module's session stream brings.
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

// ChatLine is a chat line the social module accepted and cleaned.
type ChatLine struct {
	Channel ChatChannel
	Text    string
}
