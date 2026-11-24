package gateway

import (
	"context"
	"log/slog"

	"finga/internal/application/pvp"
	model "finga/internal/model/gateway"
	"finga/internal/shared/kernel"
	"finga/pkg/hub"
	"finga/pkg/panics"
)

// PvpModule is the pvp module as the gateway calls it.
type PvpModule interface {
	Queue(ctx context.Context, p pvp.Player, mode string) error
	LeaveQueue(ctx context.Context, who kernel.CharacterID) error
	Answer(ctx context.Context, who kernel.CharacterID, offer string, accept bool) error
	Offline(ctx context.Context, who kernel.CharacterID) error
	Subscribe(gateway string) *hub.Sub[pvp.Push]
}

// Pvp is the application's pvp module.
type Pvp struct {
	p      PvpModule
	log    *slog.Logger
	panics *panics.Recorder
}

// NewPvp adapts p.
func NewPvp(p PvpModule, log *slog.Logger, rec *panics.Recorder) Pvp {
	return Pvp{p: p, log: log, panics: rec}
}

// Queue puts the player in a mode's queue.
func (p Pvp) Queue(ctx context.Context, who model.Player, mode string) error {
	return wrap(p.p.Queue(ctx, pvp.Player{ID: who.ID, Name: who.Name, Class: kernel.Class(who.Class)}, mode))
}

// LeaveQueue takes the player out of the queue.
func (p Pvp) LeaveQueue(ctx context.Context, who kernel.CharacterID) error {
	return wrap(p.p.LeaveQueue(ctx, who))
}

// Answer accepts or declines an offer.
func (p Pvp) Answer(ctx context.Context, who kernel.CharacterID, offer string, accept bool) error {
	return wrap(p.p.Answer(ctx, who, offer, accept))
}

// Offline takes a player who left out of the queue.
func (p Pvp) Offline(ctx context.Context, who kernel.CharacterID) error {
	return wrap(p.p.Offline(ctx, who))
}

// FollowDeliveries follows the module's pushes until ctx ends; a break loses only pushes, as the queue lives on.
func (p Pvp) FollowDeliveries(ctx context.Context, gateway string, deliver func(model.PvpDelivery)) {
	hub.Follow(ctx, func() *hub.Sub[pvp.Push] { return p.p.Subscribe(gateway) }, func(context.Context) error { return nil }, func(d pvp.Push) {
		if push, ok := pvpPush(d.Delivery); ok {
			deliver(model.PvpDelivery{Character: d.To, Delivery: model.Delivery{Push: push}})
		}
	}, hub.FollowOptions{Panics: p.panics, OnBreak: func(err error) {
		p.log.WarnContext(ctx, "pvp stream broke; subscribing again", "err", err)
	}})
}

// pvpPush is a pvp delivery as the gateway's push.
func pvpPush(d pvp.Delivery) (model.Push, bool) {
	switch d := d.(type) {
	case pvp.Queued:
		members := make([]string, 0, len(d.Members))
		for _, m := range d.Members {
			members = append(members, m.String())
		}
		return model.PvpQueued{Mode: d.Mode, Since: d.Since, Members: members, WaitingRoom: d.WaitingRoom}, true
	case pvp.Unqueued:
		by := make([]string, 0, len(d.By))
		for _, id := range d.By {
			by = append(by, id.String())
		}
		return model.PvpUnqueued{Reason: model.PvpUnqueueReason(d.Reason), By: by}, true
	case pvp.Offered:
		return model.PvpOffer{Offer: d.Offer, Mode: d.Mode, Bots: d.Bots, Until: d.Until}, true
	case pvp.OfferGone:
		return model.PvpOfferCancelled{Offer: d.Offer, Reason: model.PvpGoneReason(d.Reason), Requeued: d.Requeued}, true
	case pvp.Ended:
		e := model.MatchEnded{Mode: d.Mode, Result: model.MatchOutcome(d.Outcome), Battle: d.Battle, Wins: d.Wins, Score: d.Score, Left: d.Left}
		if r := d.Rating; r != nil {
			e.Rating = &model.PvpRatingChange{Before: r.Before, After: r.After, RankBefore: r.RankBefore, RankAfter: r.RankAfter, PlacementLeft: r.PlacementLeft}
		}
		return e, true
	}
	return nil, false
}
