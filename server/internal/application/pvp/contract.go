package pvp

import (
	"finga/internal/application/pvp/service"
	"finga/internal/shared/kernel"
)

// Push is something the module tells a player.
type Push struct {
	To       kernel.CharacterID
	Delivery Delivery
}

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
