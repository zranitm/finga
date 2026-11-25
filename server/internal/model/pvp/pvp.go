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
	ErrBadBuild      = errors.New("pvp: an arena build that isn't the class's")
	ErrNoCharacter   = errors.New("pvp: no such character of the account")
	ErrNotPromoted   = errors.New("pvp: only a promoted class has an arena build")
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

// Rating is a character's in a rated mode, its shown number, whether they've played their placement matches, and their record there.
type Rating struct {
	Mu, Sigma                    float64
	Shown                        int
	Placed                       bool
	Matches, Wins, Losses, Draws int
}

// Ranked is a placed character's rating on a mode's board.
type Ranked struct {
	Character kernel.CharacterID
	Rating    Rating
}

// Build is a character's arena build: eight keys, "" for an empty one, and the trait chosen for skills on it. Revision counts its saves; 0 is one never saved, the copy of the world bar.
type Build struct {
	Bar      []string
	Traits   map[string]string
	Revision int
}

// Character is a character as pvp reads them: whose they are, their class, and their world bar and traits, which a build is first copied from.
type Character struct {
	Account kernel.AccountID
	Class   kernel.Class
	Bar     []string
	Traits  map[string]string
}

// Named is a character as the board shows them.
type Named struct {
	Name  string
	Class kernel.Class
}

// Match is a match's outcome to store; Winner is 1 or 2, 0 for a draw.
type Match struct {
	ID             kernel.MatchID
	Mode           string // its mode, whose rating rows it moves, or an unrated mode's record rows it counts
	Started, Ended time.Time
	Winner         int
}

// FeatPiece is a wardrobe piece a feat gives.
type FeatPiece struct {
	Piece string
	Feat  string
}

// RankPiece is the wardrobe piece a rank gives.
type RankPiece struct {
	Piece string
	Rank  string
}

// Fighter is how one fighter of a stored match did, and their rating before and after: a character, or a bot of Tier whose rating stays.
type Fighter struct {
	Side, Slot    int // side 1 or 2
	Character     kernel.CharacterID
	Tier          string
	Left          bool
	Downs         int
	Untold        bool // away at the end, so their outcome waits for their next Welcome
	Before, After Rating
}

// Untold is a stored match a character was away from at its end and hasn't been told of: its mode, end, winner, and their side and whether they left.
type Untold struct {
	Match  kernel.MatchID
	Mode   string
	Ended  time.Time
	Winner int
	Side   int
	Left   bool
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
	Score          [2]int // a battle's
}

// Outcome is how a fighter did: a character, or a bot of Tier.
type Outcome struct {
	Character kernel.CharacterID
	Tier      string
	Left      bool
	Downs     int
	Away      bool // not in the match at its end, so told of it at their next Welcome
}
