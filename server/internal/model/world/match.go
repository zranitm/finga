package world

import (
	"errors"
	"time"

	"finga/internal/shared/kernel"
)

// The world's refusals for matches.
var (
	ErrInMatch         = errors.New("world: refused in a match")
	ErrMatchNotStarted = errors.New("world: the round hasn't started")
	ErrNotInMatch      = errors.New("world: the character is in no match")
	ErrNotPromoted     = errors.New("world: only a promoted character fights")
)

// MatchSpec is a match to open: its id and mode, and its two sides.
type MatchSpec struct {
	ID    kernel.MatchID
	Mode  string
	Sides [2][]Fighter
}

// Fighter is a place on a side: a character, or a bot when Bot is set.
type Fighter struct {
	Character kernel.CharacterID
	Bot       *Bot
}

// Bot is a made-up fighter: its id, name, class, tier and build.
type Bot struct {
	ID    kernel.CharacterID
	Name  string
	Class kernel.Class
	Tier  string
	Build Build
}

// Build is a pvp build: the skill bar's keys, "" for an empty one, and each skill's chosen trait.
type Build struct {
	Bar    []string
	Traits map[string]string
}

// Return is where a player in a match goes back to: their map, and themselves as they were there.
type Return struct {
	MapID  string
	Player Player
}

// MatchPhase is where a match stands.
type MatchPhase int

// The phases: before the first round, counting into a round, fighting it, between rounds, and the outcome.
const (
	PhasePrep MatchPhase = iota
	PhaseCountdown
	PhaseFight
	PhaseBetween
	PhaseResult
)

// MatchState is a match as its fighters see it, sent on entry and at each change.
type MatchState struct {
	Tick   uint64
	Match  kernel.MatchID
	Mode   string
	Phase  MatchPhase
	Round  int // from 1
	Wins   [2]int
	EndsIn time.Duration // the phase's time left
	Sides  [2][]MatchFighter
}

// MatchFighter is a fighter as the match lists them.
type MatchFighter struct {
	ID    kernel.CharacterID
	Name  string
	Class kernel.Class
	Bot   bool
}

func (MatchState) event() {}

// MatchResult is a match's outcome, as the world reports it: Winner is 1 or 2, 0 for a draw.
type MatchResult struct {
	Match          kernel.MatchID
	Mode           string
	Started, Ended time.Time
	Sides          [2][]FighterResult
	Winner         int
	Wins           [2]int
}

// FighterResult is how a fighter did: a character, or a bot of Tier.
type FighterResult struct {
	Character kernel.CharacterID // zero for a bot
	Tier      string             // a bot's
	Left      bool
	Downs     int
}
