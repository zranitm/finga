// Package pvp is the pvp module's model: tickets, offers, ratings and the records of matches.
package pvp

import (
	"errors"
	"time"

	"finga/internal/shared/kernel"
)

// The module's refusals.
var (
	ErrUnknownMode   = errors.New("pvp: no such mode")
	ErrAlreadyQueued = errors.New("pvp: queued already")
	ErrNotQueued     = errors.New("pvp: not queued")
	ErrOfferGone     = errors.New("pvp: the offer lapsed or was declined")
	ErrInMatch       = errors.New("pvp: in a match, or one opening")
	ErrNotLeader     = errors.New("pvp: only the party's leader queues it")
	ErrPartyTooBig   = errors.New("pvp: the party is larger than the mode allows")
	ErrPartyNotReady = errors.New("pvp: a party member can't fight now, or is queued")
	ErrPartyChanged  = errors.New("pvp: the party changed while it queued")
)

// Player is a character as they queue: the name is theirs in a match, which no bot there takes.
type Player struct {
	ID    kernel.CharacterID
	Name  string
	Class kernel.Class
}

// Party is a character's party as the queue sees it: its leader, and its members in join order.
type Party struct {
	Leader  kernel.CharacterID
	Members []Player
}

// Rating is a character's in a rated mode, its shown number, and their record there.
type Rating struct {
	Mu, Sigma                    float64
	Shown                        int
	Matches, Wins, Losses, Draws int
}

// Match is a match's outcome to store; Winner is 1 or 2, 0 for a draw.
type Match struct {
	ID             kernel.MatchID
	Mode           string // the rated mode its ratings are of
	Started, Ended time.Time
	Winner         int
}

// Fighter is how one fighter of a stored match did, and their rating before and after: a character, or a bot of Tier whose rating stays.
type Fighter struct {
	Side, Slot    int // side 1 or 2
	Character     kernel.CharacterID
	Tier          string
	Left          bool
	Downs         int
	Before, After Rating
}

// Rated is what a match's outcome does: each character's rating after it, and each fighter's row.
type Rated struct {
	After    map[kernel.CharacterID]Rating
	Fighters []Fighter
}

// Spec is a match to open: its id and mode, its two sides, and the tier of a bot that takes a team mode's leaver's place.
type Spec struct {
	ID      kernel.MatchID
	Mode    string
	Sides   [2][]Place
	StandIn string
}

// Place is a place on a side: a character, or a bot when Bot is set.
type Place struct {
	Character kernel.CharacterID
	Bot       *Bot
}

// Bot is a made-up fighter: its id, name, class, tier and pvp build's keys.
type Bot struct {
	ID    kernel.CharacterID
	Name  string
	Class kernel.Class
	Tier  string
	Bar   []string
}

// Result is a match's outcome as the world reports it; Winner is 1 or 2, 0 for a draw.
type Result struct {
	Match          kernel.MatchID
	Mode           string
	Started, Ended time.Time
	Sides          [2][]Outcome
	Winner         int
	Wins           [2]int
}

// Outcome is how a fighter did: a character, or a bot of Tier.
type Outcome struct {
	Character kernel.CharacterID
	Tier      string
	Left      bool
	Downs     int
}
