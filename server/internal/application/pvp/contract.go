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

// The deliveries, as the service makes them.
type (
	Delivery      = service.Delivery
	Queued        = service.Queued
	Unqueued      = service.Unqueued
	UnqueueReason = service.UnqueueReason
	Offered       = service.Offered
	OfferGone     = service.OfferGone
	GoneReason    = service.GoneReason
	Ended         = service.Ended
	Outcome       = service.Outcome
	RatingChange  = service.RatingChange
)

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
