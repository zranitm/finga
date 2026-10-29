package handler

import (
	"log/slog"

	characterv1 "finga/api/gen/character/v1"
	itemv1 "finga/api/gen/item/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/gamedata"
	"finga/pkg/grpcx"
	"finga/pkg/hub"
	"finga/services/world/internal/model"
)

// streamSink is one player's sink: it queues the player's events on the
// Deliver stream of the gateway the player joined through. It never blocks;
// once that stream has ended (replaced, overflowed, or the gateway gone) it
// reports false and the instance drops the player.
type streamSink struct {
	sub       *hub.Sub[*worldv1.DeliverResponse]
	character string
	log       *slog.Logger
}

// Send turns one world event into a delivery and queues it.
func (s *streamSink) Send(e model.Event) bool {
	m := s.delivery(e)
	if m == nil {
		return true // nothing the contract carries; not a reason to drop the player
	}
	m.CharacterId = s.character
	return s.sub.Send(m)
}

func (s *streamSink) delivery(e model.Event) *worldv1.DeliverResponse {
	switch ev := e.(type) {
	case model.Snapshot:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Snapshot{Snapshot: &worldv1.Snapshot{
			MapId: ev.MapID, Players: playerViews(ev.Players), Mobs: mobViews(ev.Mobs), Drops: dropViews(ev.Drops),
		}}}
	case model.Delta:
		d := &worldv1.Delta{
			AckSeq: ev.AckSeq, Players: playerViews(ev.Players), Mobs: mobViews(ev.Mobs), Drops: dropViews(ev.Drops),
			RemovedMobs: ev.RemovedMobs, RemovedDrops: ev.RemovedDrops,
		}
		for _, id := range ev.Removed {
			d.RemovedPlayers = append(d.RemovedPlayers, id.String())
		}
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Delta{Delta: d}}
	case model.Teleport:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Teleport{Teleport: &worldv1.Teleport{MapId: ev.MapID, X: ev.X, Y: ev.Y}}}
	case model.Damage:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Damage{Damage: &worldv1.Damage{
			Source: entityRef(ev.Source), Target: entityRef(ev.Target), Amount: int32(ev.Amount), Crit: ev.Crit, Miss: ev.Miss,
		}}}
	case model.Died:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Died{Died: &worldv1.Died{CharacterId: ev.ID.String(), X: ev.X, Y: ev.Y}}}
	case model.Respawned:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Respawned{Respawned: &worldv1.Respawned{
			CharacterId: ev.ID.String(), MapId: ev.MapID, X: ev.X, Y: ev.Y, Hp: int32(ev.HP),
		}}}
	case model.ExpGained:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_ExpGained{ExpGained: &worldv1.ExpGained{Amount: ev.Amount, Exp: ev.Exp}}}
	case model.LevelUp:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_LevelUp{LevelUp: &worldv1.LevelUp{Character: &worldv1.CharacterView{
			Id: ev.ID.String(), Name: ev.Name, Class: ev.Class.String(), Level: int32(ev.Level), Exp: ev.Exp, Stats: stats(ev.Stats),
			Hp: int32(ev.HP), MaxHp: int32(ev.MaxHP), Mp: int32(ev.MP), MaxMp: int32(ev.MaxMP),
		}}}}
	case model.Chat:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_Chat{Chat: &worldv1.Chat{SenderId: ev.Sender.String(), SenderName: ev.SenderName, Text: ev.Text}}}
	case model.InventoryChanged:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_InventoryChanged{InventoryChanged: &worldv1.InventoryChanged{Slots: slots(ev.Slots)}}}
	case model.PickupFailed:
		return &worldv1.DeliverResponse{Tick: ev.Tick, Event: &worldv1.DeliverResponse_PickupFailed{PickupFailed: s.pickupFailed(ev.Err)}}
	}
	return nil
}

// pickupFailed names the failure by its reason: the item service's, passed
// through, or one of the world's own from the error table. A failure with
// no reason is logged here, where its cause is known, and crosses empty.
func (s *streamSink) pickupFailed(err error) *worldv1.PickupFailed {
	st, explained := Errors.Status(err)
	if !explained {
		s.log.Error("pickup failed", "character_id", s.character, "err", err)
	}
	r, ok := grpcx.ReasonOf(st.Err())
	if !ok {
		return &worldv1.PickupFailed{}
	}
	return &worldv1.PickupFailed{ErrorDomain: r.Domain, ErrorReason: r.Name}
}

func entityRef(r model.EntityRef) *worldv1.EntityRef {
	if r.IsMob() {
		return &worldv1.EntityRef{Ref: &worldv1.EntityRef_MobId{MobId: r.Mob}}
	}
	return &worldv1.EntityRef{Ref: &worldv1.EntityRef_CharacterId{CharacterId: r.Player.String()}}
}

func stats(s gamedata.Stats) *characterv1.Stats {
	return &characterv1.Stats{Strength: int32(s.STR), Dexterity: int32(s.DEX), Intelligence: int32(s.INT), Agility: int32(s.AGI), Vitality: int32(s.VIT)}
}

func playerViews(ps []model.PlayerView) []*worldv1.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]*worldv1.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, &worldv1.PlayerView{
			Id: p.ID.String(), Name: p.Name, Class: p.Class.String(), Level: int32(p.Level), Hp: int32(p.HP), MaxHp: int32(p.MaxHP),
			Dead: p.Dead, Invulnerable: p.Invulnerable,
			X: p.State.X, Y: p.State.Y, Vx: p.State.VX, Vy: p.State.VY, Facing: int32(p.State.Facing),
			OnGround: p.State.OnGround, Climbing: p.State.Climbing,
		})
	}
	return out
}

func mobViews(ms []model.MobView) []*worldv1.MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]*worldv1.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, &worldv1.MobView{Id: m.ID, Type: m.Type, X: m.X, Y: m.Y, Facing: int32(m.Facing), Hp: int32(m.HP), MaxHp: int32(m.MaxHP)})
	}
	return out
}

func dropViews(ds []model.DropView) []*worldv1.DropView {
	if len(ds) == 0 {
		return nil
	}
	out := make([]*worldv1.DropView, 0, len(ds))
	for _, d := range ds {
		out = append(out, &worldv1.DropView{Id: d.ID, Template: d.Template, Quantity: int32(d.Quantity), X: d.X, Y: d.Y})
	}
	return out
}

// slots are the slots a committed pickup changed, in the item service's
// terms, which the gateway already reads.
func slots(in []model.Slot) []*itemv1.Slot {
	out := make([]*itemv1.Slot, 0, len(in))
	for _, s := range in {
		slot := &itemv1.Slot{Tab: string(s.Tab), Slot: int32(s.Slot)}
		if s.Item != nil {
			slot.Item = &itemv1.Item{Id: s.Item.ID.String(), TemplateId: s.Item.Template, Tab: string(s.Tab), Slot: int32(s.Slot), Quantity: int32(s.Item.Quantity)}
		}
		out = append(out, slot)
	}
	return out
}
