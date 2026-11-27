package account

import (
	"context"
	"errors"
	"finga/internal/shared/failure"
	"log/slog"

	"finga/internal/application/world"
	model "finga/internal/model/account"
	"finga/internal/shared/kernel"
	"finga/pkg/hub"
	"finga/pkg/panics"
)

// WorldModule is the world module as the server calls it.
type WorldModule interface {
	Subscribe(node string) *hub.Sub[world.Delivery]
	Join(ctx context.Context, node string, account kernel.AccountID, p world.Presence) (world.View, error)
	Leave(ctx context.Context, p world.Presence) error
	Drop(ctx context.Context, p world.Presence) error
	Say(ctx context.Context, p world.Presence, name, text string) error
}

// World is the application's World over the world module.
type World struct {
	w      WorldModule
	log    *slog.Logger
	panics *panics.Recorder
}

// NewWorld adapts the world module; log hears the stream's breaks, and rec a panic handling it.
func NewWorld(w WorldModule, log *slog.Logger, rec *panics.Recorder) World {
	return World{w: w, log: log, panics: rec}
}

// Join joins a character through one of this node's connections.
func (w World) Join(ctx context.Context, node, conn string, account kernel.AccountID, character kernel.CharacterID) (model.Joined, error) {
	v, err := w.w.Join(ctx, node, account, world.Presence{Character: character, Conn: conn})
	if err != nil {
		return model.Joined{}, worldErr(err)
	}
	return model.Joined{
		Player:     model.Player{ID: v.ID, Name: v.Name, Class: v.Class.String(), Level: v.Level, HP: v.HP, MaxHP: v.MaxHP},
		MapID:      v.MapID,
		TickRateHz: v.TickRateHz,
		JoinSeq:    v.JoinSeq,
		Waypoints:  v.Waypoints,
		View:       v,
	}, nil
}

// Leave removes the player if it joined through the presence's connection.
func (w World) Leave(ctx context.Context, p model.Presence) error {
	return worldErr(w.w.Leave(ctx, presence(p)))
}

// Drop is the presence's connection lost: a match holds the player's place a while; elsewhere it's a Leave.
func (w World) Drop(ctx context.Context, p model.Presence) error {
	return worldErr(w.w.Drop(ctx, presence(p)))
}

// Say hands a cleaned map line to the player's map.
func (w World) Say(ctx context.Context, p model.Presence, name, text string) error {
	return worldErr(w.w.Say(ctx, presence(p), name, text))
}

// FollowDeliveries follows this node's delivery stream until ctx ends; each subscription starts with WorldSubscribed, as the world dropped the players of a closed one.
func (w World) FollowDeliveries(ctx context.Context, node string, deliver func(model.WorldEvent)) {
	hub.Follow(ctx, func() *hub.Sub[world.Delivery] { return w.w.Subscribe(node) }, func(context.Context) error {
		deliver(model.WorldSubscribed{})
		return nil
	}, func(d world.Delivery) { delivered(d, deliver) }, hub.FollowOptions{Panics: w.panics, OnBreak: func(err error) {
		w.log.WarnContext(ctx, "world stream broke; subscribing again", "err", err)
	}})
}

// worldErr is a world failure as the client sees it: a class the game data lacks is the world's own fault, so internal.
func worldErr(err error) error {
	if errors.Is(err, kernel.ErrClassUnavailable) {
		return &failure.ServiceError{Public: failure.Internal, Err: err}
	}
	return wrap(err)
}

func presence(p model.Presence) world.Presence {
	return world.Presence{Character: p.Character, Conn: p.Connection}
}
