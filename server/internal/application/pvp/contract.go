package pvp

import (
	model "finga/internal/model/pvp"
	"finga/internal/shared/kernel"
)

// Push is something the module tells a player.
type Push struct {
	To       kernel.CharacterID
	Delivery Delivery
}

// Player is a character as they queue.
type Player = model.Player

// Delivery is what the module tells a player.
type Delivery = CoreDelivery

// Queued says the player is in a mode's queue.
type Queued = CoreQueued

// Unqueued says the player left the queue, and why.
type Unqueued = CoreUnqueued

// UnqueueReason is why a player left the queue.
type UnqueueReason = CoreUnqueueReason

// Offered is a match found.
type Offered = CoreOffered

// OfferGone says an offer fell through.
type OfferGone = CoreOfferGone

// GoneReason is why an offer fell through.
type GoneReason = CoreGoneReason

// Ended is a match's outcome for one of its players.
type Ended = CoreEnded

// Outcome is a match's end for a side.
type Outcome = CoreOutcome

// RatingChange is a rating's move in a match.
type RatingChange = CoreRatingChange

// PiecesAdded is the pieces a match's end gave the player's account: ranks' or win feats'.
type PiecesAdded = CorePiecesAdded

// FeatPiece is a wardrobe piece a feat gives.
type FeatPiece = model.FeatPiece

// RankPiece is the wardrobe piece a rank gives.
type RankPiece = model.RankPiece

// Profile is a character's standing in each rated mode, and their arena build.
type Profile = CoreProfile

// Standing is a character in a rated mode.
type Standing = CoreStanding

// Row is a character on a mode's board.
type Row = CoreRow

// Build is a character's arena build.
type Build = model.Build

// Missed is what a character missed while away: the newest matches that ended without them, and the older ones.
type Missed = CoreMissed

// MissedMatch is a match that ended while the character was away.
type MissedMatch = CoreMissedMatch

// The reasons and outcomes.
const (
	UnqueueLeft         = CoreUnqueueLeft
	UnqueueDeclined     = CoreUnqueueDeclined
	UnqueueLapsed       = CoreUnqueueLapsed
	UnqueuePartyChanged = CoreUnqueuePartyChanged
	GoneDeclined        = CoreGoneDeclined
	GoneLapsed          = CoreGoneLapsed
	Win                 = CoreWin
	Loss                = CoreLoss
	Draw                = CoreDraw
)
