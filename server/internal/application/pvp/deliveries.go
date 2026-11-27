package pvp

import (
	"time"

	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// CoreDelivery is what the module tells a player.
//
//sumtype:decl
type CoreDelivery interface{ delivery() }

// CoreQueued says the player is in a mode's queue, having waited Since: on joining, and again each time WaitingRoom flips, as their match waits for the node to have room for its bots, then has it.
type CoreQueued struct {
	Mode        string
	Since       time.Duration
	Members     []kernel.CharacterID
	WaitingRoom bool
}

// CoreUnqueued says the player is out of the queue, and why: By is the members whose answer took the ticket out, the one who declined or those who let the offer lapse.
type CoreUnqueued struct {
	Reason CoreUnqueueReason
	By     []kernel.CharacterID
}

// CoreUnqueueReason is why a player left the queue.
type CoreUnqueueReason int

// The reasons: they left, they declined an offer, they let it lapse, or their party changed.
const (
	CoreUnqueueLeft CoreUnqueueReason = iota
	CoreUnqueueDeclined
	CoreUnqueueLapsed
	CoreUnqueuePartyChanged
)

// CoreOffered is a match found: how many bots it has, and how long the offer stands.
type CoreOffered struct {
	Offer string
	Mode  string
	Bots  int
	Until time.Duration
}

// CoreOfferGone says an offer fell through, and whether the player is back in the queue.
type CoreOfferGone struct {
	Offer    string
	Reason   CoreGoneReason
	Requeued bool
}

// CoreGoneReason is why an offer fell through.
type CoreGoneReason int

// The reasons: someone declined, or it lapsed.
const (
	CoreGoneDeclined CoreGoneReason = iota
	CoreGoneLapsed
)

// CoreEnded is a match's outcome for one of its players, with their rating's change in a rated mode.
type CoreEnded struct {
	Mode    string
	Outcome CoreOutcome
	Battle  bool   // fought for points: Score is its, and it has no rounds
	Wins    [2]int // rounds won by each side
	Score   [2]int
	Rating  *CoreRatingChange // a rated mode's
	Left    bool              // the player left before the outcome, so lost whatever their side did
	Match   kernel.MatchID
	Away    bool // the player wasn't in the match at its end: one who hands this on live marks them told, or their next Welcome tells them
}

// CoreOutcome is a match's end for a side.
type CoreOutcome int

// The outcomes.
const (
	CoreWin CoreOutcome = iota
	CoreLoss
	CoreDraw
)

// CoreRatingChange is a mode's shown rating and rank before and after a match, and the placement matches left.
type CoreRatingChange struct {
	Before, After         int
	RankBefore, RankAfter string
	PlacementLeft         int
}

// CorePiecesAdded is the pieces a match's end gave the player's account, each new to it: ranks' or win feats'.
type CorePiecesAdded struct {
	Pieces []model.RankPiece
	Feats  []model.FeatPiece
}

func (CoreQueued) delivery()      {}
func (CoreUnqueued) delivery()    {}
func (CoreOffered) delivery()     {}
func (CoreOfferGone) delivery()   {}
func (CoreEnded) delivery()       {}
func (CorePiecesAdded) delivery() {}
