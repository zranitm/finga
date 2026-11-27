package account

import (
	"context"
	"errors"
	"finga/internal/shared/failure"
	"log/slog"
	"maps"
	"slices"
	"time"

	"finga/internal/application/world"
	model "finga/internal/model/account"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/pkg/hub"
	"finga/pkg/panics"
)

// WorldModule is the world module as the gateway calls it.
type WorldModule interface {
	Subscribe(gateway string) *hub.Sub[world.Delivery]
	Join(ctx context.Context, gateway string, account kernel.AccountID, p world.Presence) (world.View, error)
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

// Join joins a character through one of this gateway's connections.
func (w World) Join(ctx context.Context, gateway, conn string, account kernel.AccountID, character kernel.CharacterID) (model.Joined, error) {
	v, err := w.w.Join(ctx, gateway, account, world.Presence{Character: character, Conn: conn})
	if err != nil {
		return model.Joined{}, worldErr(err)
	}
	return model.Joined{
		Character: model.CharacterView{
			ID: v.ID.String(), Name: v.Name, Class: v.Class.String(), Level: v.Level, Exp: v.Exp, Stats: stats(v.Stats),
			HP: v.HP, MaxHP: v.MaxHP, Energy: v.Energy, Sheet: statSheet(v.Sheet),
		},
		MapID: v.MapID, TickRateHz: v.TickRateHz, JoinSeq: v.JoinSeq,
		Quests: questLog(v.Quests), Timers: timersLeft(v.Timers, time.Now()), Waypoints: v.Waypoints,
	}, nil
}

// questLog is the log by quest ID, nil when empty.
func questLog(log map[string]world.Quest) []model.QuestState {
	if len(log) == 0 {
		return nil
	}
	out := make([]model.QuestState, 0, len(log))
	for _, id := range slices.Sorted(maps.Keys(log)) {
		out = append(out, questState(id, log[id]))
	}
	return out
}

// timersLeft is the time left at now, to the millisecond, on each running cooldown, then each buff, by skill.
func timersLeft(t world.SkillTimers, now time.Time) []model.SkillTimer {
	out := make([]model.SkillTimer, 0, len(t.Cooldowns)+len(t.Buffs))
	for _, s := range slices.Sorted(maps.Keys(t.Cooldowns)) {
		if left := t.Cooldowns[s].Sub(now); left > 0 {
			out = append(out, model.SkillTimer{Skill: s, Kind: model.CooldownTimer, Left: left.Truncate(time.Millisecond)})
		}
	}
	for _, s := range slices.Sorted(maps.Keys(t.Buffs)) {
		if b := t.Buffs[s]; b.Until.After(now) {
			out = append(out, model.SkillTimer{Skill: s, Kind: model.BuffTimer, Left: b.Until.Sub(now).Truncate(time.Millisecond), Points: b.Points, Trait: b.Trait})
		}
	}
	return out
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

// FollowDeliveries follows this gateway's delivery stream until ctx ends; each subscription starts with WorldSubscribed, as the world dropped the players of a closed one.
func (w World) FollowDeliveries(ctx context.Context, gateway string, deliver func(model.WorldEvent)) {
	hub.Follow(ctx, func() *hub.Sub[world.Delivery] { return w.w.Subscribe(gateway) }, func(context.Context) error {
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

func stats(s gamedata.Stats) model.Stats {
	return model.Stats{STR: s.STR, DEX: s.DEX, INT: s.INT, AGI: s.AGI, VIT: s.VIT}
}
