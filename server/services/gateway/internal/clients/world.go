package clients

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/panics"
	"finga/services/gateway/internal/clients/errmap"
	"finga/services/gateway/internal/model"
)

// World is the application's World over the world service.
type World struct {
	c      worldv1.WorldServiceClient
	log    *slog.Logger
	panics *panics.Recorder
}

// NewWorld adapts the world client; log hears the stream's breaks, and rec a panic handling it.
func NewWorld(c worldv1.WorldServiceClient, log *slog.Logger, rec *panics.Recorder) World {
	return World{c: c, log: log, panics: rec}
}

// Join joins a character through one of this gateway's connections.
func (w World) Join(ctx context.Context, gateway, conn string, account kernel.AccountID, character kernel.CharacterID) (model.Joined, error) {
	res, err := w.c.Join(ctx, &worldv1.JoinRequest{GatewayId: gateway, ConnectionId: conn, AccountId: account.String(), CharacterId: character.String()})
	if err != nil {
		return model.Joined{}, errmap.Wrap(err)
	}
	c := res.GetCharacter()
	return model.Joined{
		Character: characterView(c), MapID: c.GetMapId(), X: c.GetX(), Y: c.GetY(), TickRateHz: int(res.GetTickRateHz()), JoinSeq: res.GetJoinSeq(),
		Quests: questStates(res.GetQuests()),
	}, nil
}

// Leave removes the player if it joined through the presence's connection.
func (w World) Leave(ctx context.Context, p model.Presence) error {
	_, err := w.c.Leave(ctx, &worldv1.LeaveRequest{Presence: presence(p)})
	return errmap.Wrap(err)
}

// Input sends one input.
func (w World) Input(ctx context.Context, p model.Presence, seq uint32, k model.Keys) error {
	_, err := w.c.Input(ctx, &worldv1.InputRequest{Presence: presence(p), Seq: seq, Keys: &worldv1.Keys{Left: k.Left, Right: k.Right, Up: k.Up, Down: k.Down, Jump: k.Jump}})
	return errmap.Wrap(err)
}

// Attack attacks.
func (w World) Attack(ctx context.Context, p model.Presence) error {
	_, err := w.c.Attack(ctx, &worldv1.AttackRequest{Presence: presence(p)})
	return errmap.Wrap(err)
}

// EnterPortal takes the portal the player stands in.
func (w World) EnterPortal(ctx context.Context, p model.Presence) error {
	_, err := w.c.EnterPortal(ctx, &worldv1.EnterPortalRequest{Presence: presence(p)})
	return errmap.Wrap(err)
}

// Equip wears an item; it answers once the world applied the new stats.
func (w World) Equip(ctx context.Context, p model.Presence, in model.ItemEquip) error {
	_, err := w.c.Equip(ctx, &worldv1.EquipRequest{Presence: presence(p), IdempotencyKey: in.IdempotencyKey, ItemId: in.Item.String(), EquipSlot: in.EquipSlot})
	return errmap.Wrap(err)
}

// Unequip takes a worn item off; it answers once the world applied the new stats.
func (w World) Unequip(ctx context.Context, p model.Presence, in model.ItemUnequip) error {
	req := &worldv1.UnequipRequest{Presence: presence(p), IdempotencyKey: in.IdempotencyKey, EquipSlot: in.EquipSlot}
	if in.ToSlot != nil {
		to := int32(*in.ToSlot) //nolint:gosec // a tab has few slots
		req.ToSlot = &to
	}
	_, err := w.c.Unequip(ctx, req)
	return errmap.Wrap(err)
}

// UseItem drinks a potion; it answers once the world applied what it restores.
func (w World) UseItem(ctx context.Context, p model.Presence, in model.ItemUse) error {
	_, err := w.c.UseItem(ctx, &worldv1.UseItemRequest{Presence: presence(p), IdempotencyKey: in.IdempotencyKey, ItemId: in.Item.String(), Template: in.Template})
	return errmap.Wrap(err)
}

// SpendPoints adds points to the stats; it answers once the world applied them.
func (w World) SpendPoints(ctx context.Context, p model.Presence, in model.PointsSpend) error {
	pts := &worldv1.Stats{Strength: int32(in.Points.STR), Dexterity: int32(in.Points.DEX), Intelligence: int32(in.Points.INT), Agility: int32(in.Points.AGI), Vitality: int32(in.Points.VIT)} //nolint:gosec // they came in as int32
	_, err := w.c.SpendPoints(ctx, &worldv1.SpendPointsRequest{Presence: presence(p), ExpectedUnspent: int32(in.ExpectedUnspent), Points: pts})                                               //nolint:gosec // they came in as int32
	return errmap.Wrap(err)
}

// SpendSkillPoints invests skill points; it answers once the world applied them.
func (w World) SpendSkillPoints(ctx context.Context, p model.Presence, in model.SkillPointsSpend) error {
	pts := make(map[string]int32, len(in.Points))
	for skill, n := range in.Points {
		pts[skill] = int32(n) //nolint:gosec // they came in as int32
	}
	_, err := w.c.SpendSkillPoints(ctx, &worldv1.SpendSkillPointsRequest{Presence: presence(p), ExpectedUnspent: int32(in.ExpectedUnspent), Points: pts}) //nolint:gosec // it came in as int32
	return errmap.Wrap(err)
}

// UseSkill uses an active skill; it answers once the world's tick resolved it.
func (w World) UseSkill(ctx context.Context, p model.Presence, skill string) error {
	_, err := w.c.UseSkill(ctx, &worldv1.UseSkillRequest{Presence: presence(p), Skill: skill})
	return errmap.Wrap(err)
}

// ResetPoints uses the free reset; it answers once the world applied it.
func (w World) ResetPoints(ctx context.Context, p model.Presence, expectedUnspent int) error {
	_, err := w.c.ResetPoints(ctx, &worldv1.ResetPointsRequest{Presence: presence(p), ExpectedUnspent: int32(expectedUnspent)}) //nolint:gosec // it came in as int32
	return errmap.Wrap(err)
}

// Say hands a cleaned map line to the player's map.
func (w World) Say(ctx context.Context, p model.Presence, name, text string) error {
	_, err := w.c.Say(ctx, &worldv1.SayRequest{Presence: presence(p), SenderName: name, Text: text})
	return errmap.Wrap(err)
}

// Talk opens an NPC's dialog; it answers once the NpcDialog is on its way.
func (w World) Talk(ctx context.Context, p model.Presence, npc string) error {
	_, err := w.c.Talk(ctx, &worldv1.TalkRequest{Presence: presence(p), NpcId: npc})
	return errmap.Wrap(err)
}

// Promote moves the character to the class the NPC within reach trains; it answers once applied.
func (w World) Promote(ctx context.Context, p model.Presence, npc, class string) error {
	_, err := w.c.Promote(ctx, &worldv1.PromoteRequest{Presence: presence(p), NpcId: npc, Class: class})
	return errmap.Wrap(err)
}

// AcceptQuest takes a quest an NPC gives; it answers once the quest is in the log.
func (w World) AcceptQuest(ctx context.Context, p model.Presence, in model.QuestAsk) error {
	_, err := w.c.AcceptQuest(ctx, &worldv1.AcceptQuestRequest{Presence: presence(p), NpcId: in.NPC, QuestId: in.Quest})
	return errmap.Wrap(err)
}

// AbandonQuest drops an active quest and its progress.
func (w World) AbandonQuest(ctx context.Context, p model.Presence, quest string) error {
	_, err := w.c.AbandonQuest(ctx, &worldv1.AbandonQuestRequest{Presence: presence(p), QuestId: quest})
	return errmap.Wrap(err)
}

// HandInQuest hands a ready quest in to an NPC; it answers once its items and EXP applied.
func (w World) HandInQuest(ctx context.Context, p model.Presence, in model.QuestAsk) error {
	_, err := w.c.HandInQuest(ctx, &worldv1.HandInQuestRequest{Presence: presence(p), NpcId: in.NPC, QuestId: in.Quest})
	return errmap.Wrap(err)
}

// FollowDeliveries follows this gateway's delivery stream until ctx ends.
func (w World) FollowDeliveries(ctx context.Context, gateway string, deliver func(model.WorldEvent)) {
	grpcx.Follow(ctx, func(ctx context.Context) (grpc.ServerStreamingClient[worldv1.DeliverResponse], error) {
		return w.c.Deliver(ctx, &worldv1.DeliverRequest{GatewayId: gateway})
	}, func(m *worldv1.DeliverResponse) { delivered(m, deliver) }, grpcx.FollowOptions{Panics: w.panics, OnBreak: func(err error) {
		w.log.WarnContext(ctx, "world stream broke; subscribing again", "err", err)
	}})
}

// delivered turns one message of the stream into its events: a Frame into one per recipient, all sharing it.
func delivered(m *worldv1.DeliverResponse, deliver func(model.WorldEvent)) {
	if m.GetSubscribed() != nil {
		deliver(model.WorldSubscribed{})
		return
	}
	if f := m.GetFrame(); f != nil {
		frame := frameOf(f.GetDelta())
		for _, r := range f.GetRecipients() {
			character, _ := kernel.ParseCharacterID(r.GetCharacterId()) // zero if bad: the delivery still routes by connection
			deliver(model.WorldDelivery{Connection: r.GetConnectionId(), Character: character, Delivery: model.Delivery{Tick: m.GetTick(), Push: model.Delta{Frame: frame, AckSeq: r.GetAckSeq()}}})
		}
		return
	}
	character, _ := kernel.ParseCharacterID(m.GetCharacterId()) // zero if bad: the delivery still routes by connection
	if m.GetDetached() != nil {
		deliver(model.Detached{Connection: m.GetConnectionId(), Character: character})
		return
	}
	p, ok := worldPush(m)
	if !ok {
		return
	}
	deliver(model.WorldDelivery{Connection: m.GetConnectionId(), Character: character, Delivery: model.Delivery{Tick: m.GetTick(), Push: p}})
}

func presence(p model.Presence) *worldv1.Presence {
	return &worldv1.Presence{CharacterId: p.Character.String(), ConnectionId: p.Connection}
}
