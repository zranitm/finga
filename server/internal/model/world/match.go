package world

import (
	"encoding/binary"
	"errors"
	"math/rand/v2"
	"slices"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

// The world's refusals for matches.
var (
	ErrInMatch         = errors.New("world: refused in a match")
	ErrMatchNotStarted = errors.New("world: the round hasn't started")
	ErrNotOnArenaBar   = errors.New("world: in a match, a skill off the arena bar")
	ErrNotInMatch      = errors.New("world: the character is in no match")
	ErrNotPromoted     = errors.New("world: only a promoted character fights")
)

// MatchSpec is a match to open: its id and mode, and its two sides.
type MatchSpec struct {
	ID      kernel.MatchID
	Mode    string
	Sides   [2][]Fighter
	StandIn string // the tier of a bot that takes a team mode's leaver's place
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

// Build is a pvp build: the skill bar's keys, "" for an empty one, each skill's chosen trait, and a player's revision, which counts their saves; a bot's is 0.
type Build struct {
	Bar      []string
	Traits   map[string]string
	Revision int
}

// Return is where a player in a match goes back to: their map, and themselves as they were there.
type Return struct {
	MapID   string
	Channel int // the region's channel they left, which they go back to while it has room
	Player  Player
}

// MatchPhase is where a match stands.
type MatchPhase int

// The phases: before the first round or the battle, counting into a round, fighting, between rounds, and the outcome. A battle has no count or rounds.
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
	Round  int // from 1; 0 in a battle
	Wins   [2]int
	EndsIn time.Duration // the phase's time left
	Sides  [2][]MatchFighter
	Battle bool // fought for points: Score and Points are its, and it has no rounds
	Score  [2]int
	Points []CapturePoint // in the map's order
}

// CapturePoint is a battle's point as it stands: the side holding it, 0 for none; its progress from -1, side 1's, through 0 to 1, side 2's; and whether both sides stand in it, holding it still.
type CapturePoint struct {
	ID        string
	Holder    int
	Progress  float64
	Contested bool
}

// MatchFighter is a fighter as the match lists them.
type MatchFighter struct {
	ID    kernel.CharacterID
	Name  string
	Class kernel.Class
	Bot   bool
	Away  bool // their connection dropped, and their place is held for them (pvp.md section 4)
}

func (MatchState) event() {}

// ArenaBar is the bar a match puts on its player at a countdown: from then, their skills go from its keys alone.
type ArenaBar struct {
	Tick uint64
	Bar  []string
}

func (ArenaBar) event() {}

// MatchResult is a match's outcome, as the world reports it: Winner is 1 or 2, 0 for a draw.
type MatchResult struct {
	Match          kernel.MatchID
	Mode           string
	Started, Ended time.Time
	Sides          [2][]FighterResult
	Winner         int
	Wins           [2]int
	Score          [2]int // a battle's
}

// FighterResult is how a fighter did: a character, or a bot of Tier.
type FighterResult struct {
	Character kernel.CharacterID // zero for a bot
	Tier      string             // a bot's
	Left      bool
	Downs     int
}

// BotLook is a bot's look, the same for one id: each slot its class's own half the time, else a piece of that slot every source of which is a boss, and a weapon look of weapon's kind. A rank, quest or feat piece never shows on a bot, so a rank piece always means a player earned it; feat pieces join with feats.
func BotLook(pieces []gamedata.Piece, weapon gamedata.WeaponKind, id kernel.CharacterID) Look {
	seed := [16]byte(id)
	r := rand.New(rand.NewPCG(binary.BigEndian.Uint64(seed[:8]), binary.BigEndian.Uint64(seed[8:]))) //nolint:gosec // a bot's looks, not a secret
	worn := map[gamedata.LookSlot][]string{}
	for _, p := range pieces {
		if dropped(p) && (p.Slot != gamedata.WeaponSlot || p.Kind == weapon) {
			worn[p.Slot] = append(worn[p.Slot], p.ID)
		}
	}
	pick := func(slot gamedata.LookSlot) string {
		if len(worn[slot]) == 0 || r.IntN(2) == 0 {
			return "" // the class's own
		}
		return worn[slot][r.IntN(len(worn[slot]))]
	}
	return Look{Outfit: pick(gamedata.OutfitSlot), Headwear: pick(gamedata.HeadwearSlot), Face: pick(gamedata.FaceSlot), Weapon: pick(gamedata.WeaponSlot), Wings: pick(gamedata.WingsSlot), Revision: 1}
}

// dropped is whether every source of p is a boss.
func dropped(p gamedata.Piece) bool {
	return len(p.From) > 0 && !slices.ContainsFunc(p.From, func(s gamedata.PieceSource) bool { return s.Boss == "" })
}
