package pvp

import (
	"finga/internal/application/pvp/service"
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
type Delivery = service.Delivery

// Queued says the player is in a mode's queue.
type Queued = service.Queued

// Unqueued says the player left the queue, and why.
type Unqueued = service.Unqueued

// UnqueueReason is why a player left the queue.
type UnqueueReason = service.UnqueueReason

// Offered is a match found.
type Offered = service.Offered

// OfferGone says an offer fell through.
type OfferGone = service.OfferGone

// GoneReason is why an offer fell through.
type GoneReason = service.GoneReason

// Ended is a match's outcome for one of its players.
type Ended = service.Ended

// Outcome is a match's end for a side.
type Outcome = service.Outcome

// RatingChange is a rating's move in a match.
type RatingChange = service.RatingChange

// PiecesAdded is the ranks' pieces a match's end gave the player's account.
type PiecesAdded = service.PiecesAdded

// RankPiece is the wardrobe piece a rank gives.
type RankPiece = model.RankPiece

// Profile is a character's standing in each rated mode, and their arena build.
type Profile = service.Profile

// Standing is a character in a rated mode.
type Standing = service.Standing

// Row is a character on a mode's board.
type Row = service.Row

// Build is a character's arena build.
type Build = model.Build

// The reasons and outcomes.
const (
	UnqueueLeft         = service.UnqueueLeft
	UnqueueDeclined     = service.UnqueueDeclined
	UnqueueLapsed       = service.UnqueueLapsed
	UnqueuePartyChanged = service.UnqueuePartyChanged
	GoneDeclined        = service.GoneDeclined
	GoneLapsed          = service.GoneLapsed
	Win                 = service.Win
	Loss                = service.Loss
	Draw                = service.Draw
)
