package account

import (
	"context"
	"log/slog"

	"finga/internal/application/item"
	"finga/internal/application/pvp"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
	"finga/pkg/hub"
	"finga/pkg/panics"
)

// PvpModule is the pvp module as the gateway calls it.
type PvpModule interface {
	Offline(ctx context.Context, who kernel.CharacterID) error
	Missed(ctx context.Context, id kernel.CharacterID) (pvp.Missed, error)
	Told(ctx context.Context, id kernel.CharacterID, matches []kernel.MatchID) error
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

// Offline takes a player who left out of the queue.
func (p Pvp) Offline(ctx context.Context, who kernel.CharacterID) error {
	return wrap(p.p.Offline(ctx, who))
}

// Missed takes the matches that ended while the player was away, for their Welcome.
func (p Pvp) Missed(ctx context.Context, who kernel.CharacterID) (model.Missed, error) {
	m, err := p.p.Missed(ctx, who)
	if err != nil {
		return model.Missed{}, wrap(err)
	}
	out := model.Missed{Older: m.Older}
	for _, x := range m.Matches {
		out.Matches = append(out.Matches, model.MissedMatch{Match: x.Match, Mode: x.Mode, Result: model.MatchOutcome(x.Outcome), Left: x.Left, Ended: x.Ended})
	}
	return out, nil
}

// Told marks the player told of matches, their outcomes handed to them.
func (p Pvp) Told(ctx context.Context, who kernel.CharacterID, matches []kernel.MatchID) error {
	return wrap(p.p.Told(ctx, who, matches))
}

// FollowDeliveries follows the module's pushes until ctx ends; a break loses only pushes, as the queue lives on.
func (p Pvp) FollowDeliveries(ctx context.Context, gateway string, deliver func(model.PvpDelivery)) {
	hub.Follow(ctx, func() *hub.Sub[pvp.Push] { return p.p.Subscribe(gateway) }, func(context.Context) error { return nil }, func(d pvp.Push) {
		if push, ok := pvpPush(d.Delivery); ok {
			out := model.PvpDelivery{Character: d.To, Delivery: model.Delivery{Event: push}}
			if e, ok := d.Delivery.(pvp.Ended); ok && e.Away {
				out.Untold = e.Match
			}
			deliver(out)
		}
	}, hub.FollowOptions{Panics: p.panics, OnBreak: func(err error) {
		p.log.WarnContext(ctx, "pvp stream broke; subscribing again", "err", err)
	}})
}

// pvpPush is a pvp delivery as the gateway's push.
func pvpPush(d pvp.Delivery) (any, bool) {
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
	case pvp.PiecesAdded:
		pieces := make([]model.AddedPiece, 0, len(d.Pieces))
		for _, p := range d.Pieces {
			pieces = append(pieces, model.AddedPiece{ID: p.Piece, Cause: item.PieceFromRank})
		}
		for _, p := range d.Feats {
			pieces = append(pieces, model.AddedPiece{ID: p.Piece, Cause: item.PieceFromFeat})
		}
		return model.PiecesAdded{Pieces: pieces}, true
	}
	return nil, false
}
