package local

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/internal/sim"
	"finga/pkg/hub"
	"finga/pkg/panics"
	"finga/services/gateway/internal/model"
	"finga/services/world"
)

// WorldModule is the world module as the gateway calls it.
type WorldModule interface {
	Subscribe(gateway string) *hub.Sub[world.Delivery]
	Join(ctx context.Context, gateway string, account kernel.AccountID, p world.Presence) (world.View, error)
	Leave(ctx context.Context, p world.Presence) error
	Input(ctx context.Context, p world.Presence, seq uint32, in sim.Input, view uint32) error
	Attack(ctx context.Context, p world.Presence, seq uint32) error
	EnterPortal(ctx context.Context, p world.Presence) error
	Say(ctx context.Context, p world.Presence, name, text string) error
	Equip(ctx context.Context, p world.Presence, key string, item kernel.ItemID, equipSlot string) error
	UseItem(ctx context.Context, p world.Presence, key string, item kernel.ItemID, template string) error
	Unequip(ctx context.Context, p world.Presence, key, equipSlot string, toSlot *int) error
	SpendPoints(ctx context.Context, p world.Presence, expected int, points gamedata.Stats) error
	ResetPoints(ctx context.Context, p world.Presence, expected int) error
	SpendSkillPoints(ctx context.Context, p world.Presence, expected int, points map[string]int) error
	UseSkill(ctx context.Context, p world.Presence, skill string, seq uint32) error
	Promote(ctx context.Context, p world.Presence, npc string, class kernel.Class) error
	Talk(ctx context.Context, p world.Presence, npc string) error
	AcceptQuest(ctx context.Context, p world.Presence, npc, quest string) error
	AbandonQuest(ctx context.Context, p world.Presence, quest string) error
	HandInQuest(ctx context.Context, p world.Presence, npc, quest string) error
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
		MapID: v.MapID, X: v.X, Y: v.Y, TickRateHz: v.TickRateHz, JoinSeq: v.JoinSeq,
		Quests: questLog(v.Quests), Timers: timersLeft(v.Timers, time.Now()),
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
			out = append(out, model.SkillTimer{Skill: s, Kind: model.BuffTimer, Left: b.Until.Sub(now).Truncate(time.Millisecond), Points: b.Points})
		}
	}
	return out
}

// Leave removes the player if it joined through the presence's connection.
func (w World) Leave(ctx context.Context, p model.Presence) error {
	return worldErr(w.w.Leave(ctx, presence(p)))
}

// Input sends one input.
func (w World) Input(ctx context.Context, p model.Presence, seq uint32, k model.Keys, view uint32) error {
	return worldErr(w.w.Input(ctx, presence(p), seq, sim.Input{Left: k.Left, Right: k.Right, Up: k.Up, Down: k.Down, Jump: k.Jump}, view))
}

// Attack attacks.
func (w World) Attack(ctx context.Context, p model.Presence, seq uint32) error {
	return worldErr(w.w.Attack(ctx, presence(p), seq))
}

// EnterPortal takes the portal the player stands in.
func (w World) EnterPortal(ctx context.Context, p model.Presence) error {
	return worldErr(w.w.EnterPortal(ctx, presence(p)))
}

// Equip wears an item; it answers once the world applied the new stats.
func (w World) Equip(ctx context.Context, p model.Presence, in model.ItemEquip) error {
	return worldErr(w.w.Equip(ctx, presence(p), in.IdempotencyKey, in.Item, in.EquipSlot))
}

// Unequip takes a worn item off; it answers once the world applied the new stats.
func (w World) Unequip(ctx context.Context, p model.Presence, in model.ItemUnequip) error {
	var to *int
	if in.ToSlot != nil {
		slot := *in.ToSlot
		to = &slot
	}
	return worldErr(w.w.Unequip(ctx, presence(p), in.IdempotencyKey, in.EquipSlot, to))
}

// UseItem drinks a potion; it answers once the world applied what it restores.
func (w World) UseItem(ctx context.Context, p model.Presence, in model.ItemUse) error {
	return worldErr(w.w.UseItem(ctx, presence(p), in.IdempotencyKey, in.Item, in.Template))
}

// SpendPoints adds points to the stats; it answers once the world applied them.
func (w World) SpendPoints(ctx context.Context, p model.Presence, in model.PointsSpend) error {
	pts := gamedata.Stats{STR: in.Points.STR, DEX: in.Points.DEX, INT: in.Points.INT, AGI: in.Points.AGI, VIT: in.Points.VIT}
	return worldErr(w.w.SpendPoints(ctx, presence(p), in.ExpectedUnspent, pts))
}

// SpendSkillPoints invests skill points; it answers once the world applied them.
func (w World) SpendSkillPoints(ctx context.Context, p model.Presence, in model.SkillPointsSpend) error {
	pts := make(map[string]int, len(in.Points))
	maps.Copy(pts, in.Points)
	return worldErr(w.w.SpendSkillPoints(ctx, presence(p), in.ExpectedUnspent, pts))
}

// UseSkill uses an active skill; it answers once the world's tick resolved it.
func (w World) UseSkill(ctx context.Context, p model.Presence, skill string, seq uint32) error {
	return worldErr(w.w.UseSkill(ctx, presence(p), skill, seq))
}

// ResetPoints uses the free reset; it answers once the world applied it.
func (w World) ResetPoints(ctx context.Context, p model.Presence, expectedUnspent int) error {
	return worldErr(w.w.ResetPoints(ctx, presence(p), expectedUnspent))
}

// Say hands a cleaned map line to the player's map.
func (w World) Say(ctx context.Context, p model.Presence, name, text string) error {
	return worldErr(w.w.Say(ctx, presence(p), name, text))
}

// Talk opens an NPC's dialog; it answers once the NpcDialog is on its way.
func (w World) Talk(ctx context.Context, p model.Presence, npc string) error {
	return worldErr(w.w.Talk(ctx, presence(p), npc))
}

// Promote moves the character to the class the NPC within reach trains; it answers once applied.
func (w World) Promote(ctx context.Context, p model.Presence, npc, class string) error {
	return worldErr(w.w.Promote(ctx, presence(p), npc, kernel.Class(class)))
}

// AcceptQuest takes a quest an NPC gives; it answers once the quest is in the log.
func (w World) AcceptQuest(ctx context.Context, p model.Presence, in model.QuestAsk) error {
	return worldErr(w.w.AcceptQuest(ctx, presence(p), in.NPC, in.Quest))
}

// AbandonQuest drops an active quest and its progress.
func (w World) AbandonQuest(ctx context.Context, p model.Presence, quest string) error {
	return worldErr(w.w.AbandonQuest(ctx, presence(p), quest))
}

// HandInQuest hands a ready quest in to an NPC; it answers once its items and EXP applied.
func (w World) HandInQuest(ctx context.Context, p model.Presence, in model.QuestAsk) error {
	return worldErr(w.w.HandInQuest(ctx, presence(p), in.NPC, in.Quest))
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

// worldErr is a world failure as the client saw it over gRPC: a class the game data lacks is the world's own fault, so internal.
func worldErr(err error) error {
	if errors.Is(err, kernel.ErrClassUnavailable) {
		return &model.ServiceError{Public: model.Internal, Err: err}
	}
	return wrap(err)
}

func presence(p model.Presence) world.Presence {
	return world.Presence{Character: p.Character, Conn: p.Connection}
}
