package account

import (
	"context"
	"log/slog"

	"finga/internal/application/pvp"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
	"finga/pkg/hub"
	"finga/pkg/panics"
)

// PvpModule is the pvp module as the server calls it.
type PvpModule interface {
	Offline(ctx context.Context, who kernel.CharacterID) error
	Missed(ctx context.Context, id kernel.CharacterID) (pvp.Missed, error)
	Told(ctx context.Context, id kernel.CharacterID, matches []kernel.MatchID) error
	Subscribe(node string) *hub.Sub[pvp.Push]
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

// Missed takes the matches that ended while the player was away, for their Welcome: which are told once it is written, and the module's own account of them.
func (p Pvp) Missed(ctx context.Context, who kernel.CharacterID) (model.MissedMatches, error) {
	m, err := p.p.Missed(ctx, who)
	if err != nil {
		return model.MissedMatches{}, wrap(err)
	}
	told := make([]kernel.MatchID, 0, len(m.Matches)+len(m.Older))
	for _, x := range m.Matches {
		told = append(told, x.Match)
	}
	return model.MissedMatches{Told: append(told, m.Older...), Details: m}, nil
}

// Told marks the player told of matches, their outcomes handed to them.
func (p Pvp) Told(ctx context.Context, who kernel.CharacterID, matches []kernel.MatchID) error {
	return wrap(p.p.Told(ctx, who, matches))
}

// FollowDeliveries follows the module's pushes until ctx ends; a break loses only pushes, as the queue lives on.
func (p Pvp) FollowDeliveries(ctx context.Context, node string, deliver func(model.PvpDelivery)) {
	hub.Follow(ctx, func() *hub.Sub[pvp.Push] { return p.p.Subscribe(node) }, func(context.Context) error { return nil }, func(d pvp.Push) {
		deliver(pvpDelivery(d))
	}, hub.FollowOptions{Panics: p.panics, OnBreak: func(err error) {
		p.log.WarnContext(ctx, "pvp stream broke; subscribing again", "err", err)
	}})
}

// pvpDelivery is a pvp push as the connection flows route it: to the player it names, as the module sent it, with the match an away player was told of marked.
func pvpDelivery(d pvp.Push) model.PvpDelivery {
	out := model.PvpDelivery{Character: d.To, Delivery: model.Delivery{Event: d.Delivery}}
	if e, ok := d.Delivery.(pvp.Ended); ok && e.Away {
		out.Untold = e.Match
	}
	return out
}
