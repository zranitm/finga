package service

import (
	"time"

	"finga/internal/shared/kernel"
)

// Delivery is what the module tells a player.
//
//sumtype:decl
type Delivery interface{ delivery() }

// Queued says the player is in a mode's queue, having waited Since.
type Queued struct {
	Mode    string
	Since   time.Duration
	Members []kernel.CharacterID
}

// Unqueued says the player is out of the queue, and why: By is the members whose answer took the ticket out, the one who declined or those who let the offer lapse.
type Unqueued struct {
	Reason UnqueueReason
	By     []kernel.CharacterID
}

// UnqueueReason is why a player left the queue.
type UnqueueReason int

// The reasons: they left, they declined an offer, they let it lapse, or their party changed.
const (
	UnqueueLeft UnqueueReason = iota
	UnqueueDeclined
	UnqueueLapsed
	UnqueuePartyChanged
)

// Offered is a match found: how many bots it has, and how long the offer stands.
type Offered struct {
	Offer string
	Mode  string
	Bots  int
	Until time.Duration
}

// OfferGone says an offer fell through, and whether the player is back in the queue.
type OfferGone struct {
	Offer    string
	Reason   GoneReason
	Requeued bool
}

// GoneReason is why an offer fell through.
type GoneReason int

// The reasons: someone declined, or it lapsed.
const (
	GoneDeclined GoneReason = iota
	GoneLapsed
)

// Ended is a match's outcome for one of its players, with their rating's change in a rated mode.
type Ended struct {
	Mode    string
	Outcome Outcome
	Battle  bool   // fought for points: Score is its, and it has no rounds
	Wins    [2]int // rounds won by each side
	Score   [2]int
	Rating  *RatingChange // a rated mode's
	Left    bool          // the player left before the outcome, so lost whatever their side did
}

// Outcome is a match's end for a side.
type Outcome int

// The outcomes.
const (
	Win Outcome = iota
	Loss
	Draw
)

// RatingChange is a mode's shown rating and rank before and after a match, and the placement matches left.
type RatingChange struct {
	Before, After         int
	RankBefore, RankAfter string
	PlacementLeft         int
}

func (Queued) delivery()    {}
func (Unqueued) delivery()  {}
func (Offered) delivery()   {}
func (OfferGone) delivery() {}
func (Ended) delivery()     {}
