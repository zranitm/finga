package gamedata

import (
	"fmt"
	"slices"
	"time"

	"finga/internal/shared/kernel"
)

// BotMemory is how far back a match's bots see the fighters: a tier's reaction is under it.
const BotMemory = time.Second

// BarKeys is how many keys a skill bar has, the world's and a match's alike.
const BarKeys = 8

// PvpRules are data/pvp.yaml: the match modes, the queue, the rules of rounds, the kit everyone fights with, ratings and ranks, and the bots (docs/tech/pvp.md section 3).
type PvpRules struct {
	Modes  []PvpMode // by team size
	Queue  PvpQueue
	Rounds PvpRounds
	Result time.Duration // how long the outcome shows before everyone goes back
	Kits   map[kernel.Class]Kit
	Scale  PvpScale
	Rating RatingRules
	Ranks  []Rank // rising from 0
	Bots   BotRules
}

// PvpMode is one way to fight: Team a side, a party ticket up to Party, on Map.
type PvpMode struct {
	ID         string
	Name       string // what players see, as 1v1
	Team       int
	Party      int
	Map        string
	Rated      bool
	RatingFrom string     // an unrated mode's ticket reads this mode's rating
	Rounds     ModeRounds // a round mode's
}

// ModeRounds is a round mode's: the first to win more than half of BestOf takes the match, and Most rounds end it a draw; each round lasts Length.
type ModeRounds struct {
	BestOf, Most int
	Length       time.Duration
}

// PvpQueue is how the matcher waits: bots fill a ticket's places after BotsAfter, an offer lapses after Offer, and a ticket's window in mu widens from Start by PerSecond to Most.
type PvpQueue struct {
	BotsAfter, Offer time.Duration
	Window           QueueWindow
}

// QueueWindow is a ticket's reach in mu: Start + PerSecond × seconds waited, at most Most.
type QueueWindow struct {
	Start, PerSecond, Most float64
}

// PvpRounds are a round mode's phases: Prep before the first round, Countdown into each, Between after each.
type PvpRounds struct {
	Prep, Countdown, Between time.Duration
}

// Kit is what a class fights with in a match: its own stats at Level, spent by its auto-assign split, and the best item it can wear in each equip slot; and its own factors on its max HP and the hits it deals, beside the PvpScale, which the balance harness tunes.
type Kit struct {
	Level      int
	Own        Stats
	Worn       []KitItem // one per equip slot, in slot order
	HP, Damage float64
}

// KitItem is the item a kit wears in an equip slot, its stats at the middle of their rolls.
type KitItem struct {
	Slot string
	Item string
}

// PvpScale multiplies a kit's max HP and every hit between players, as the balance harness tunes them.
type PvpScale struct {
	HP, Damage float64
}

// RatingRules are Weng-Lin's: a new rating's Mu and Sigma, the skill gap Beta, and Tau added to sigma before each update; the shown rating is Base + PerPoint × (mu − 3 sigma), past Placement matches.
type RatingRules struct {
	Mu, Sigma, Beta, Tau float64
	Shown                ShownRating
	Placement            int
}

// ShownRating is how mu and sigma read as one number.
type ShownRating struct {
	Base, PerPoint float64
}

// Rank is a shown rating's band, from From up.
type Rank struct {
	ID   string
	From int
}

// BotRules are the bots: at most Most on a node at once, their tiers by strength, their names, and each class's builds.
type BotRules struct {
	Most   int
	Tiers  []BotTier // mu rising
	Names  []string
	Builds map[kernel.Class][]BotBuild
}

// BotTier is how well a bot plays: its rating Mu, how late it sees, how often it defends and chains, how soon it breaks free, and how far off it aims, in radians.
type BotTier struct {
	ID        string
	Mu        float64
	Reaction  time.Duration
	Defend    float64
	Chain     float64
	BreakFree time.Duration
	Aim       float64
}

// BotBuild is a pvp build and the order a bot uses its skills in.
type BotBuild struct {
	Bar    []string // eight keys, "" for an empty one
	Traits map[string]string
	Order  []string
}

// PvpBuildFault is what's wrong with a pvp build for c, nil for none: BarKeys keys, each empty or one of c's active skills and none twice, and each trait one of its own skill's, for a skill on the bar.
func PvpBuildFault(c Class, skill func(string) (Skill, bool), bar []string, traits map[string]string) error {
	if len(bar) != BarKeys {
		return fmt.Errorf("%d keys, not %d", len(bar), BarKeys)
	}
	for i, id := range bar {
		if id == "" {
			continue
		}
		s, ok := skill(id)
		switch {
		case !ok || s.Kind != Active || !c.Learnable(s):
			return fmt.Errorf("key %d: %q is no active skill of %s", i+1, id, c.ID)
		case slices.Contains(bar[:i], id):
			return fmt.Errorf("key %d: %s is on another key too", i+1, id)
		}
	}
	for id, trait := range traits {
		s, ok := skill(id)
		if !ok || !slices.Contains(bar, id) {
			return fmt.Errorf("a trait for %q, no skill on the bar", id)
		}
		if _, ok := s.Trait(trait); !ok {
			return fmt.Errorf("%s has no trait %q", id, trait)
		}
	}
	return nil
}

// Pvp is the match rules.
func (d *Data) Pvp() PvpRules { return d.p.Pvp }

// PvpMode finds a mode by its ID.
func (d *Data) PvpMode(id string) (PvpMode, bool) {
	for _, m := range d.p.Pvp.Modes {
		if m.ID == id {
			return m, true
		}
	}
	return PvpMode{}, false
}

// Kit is the class's kit; a Beginner has none, since only promoted characters fight.
func (d *Data) Kit(class kernel.Class) (Kit, bool) {
	k, ok := d.p.Pvp.Kits[class]
	return k, ok
}
