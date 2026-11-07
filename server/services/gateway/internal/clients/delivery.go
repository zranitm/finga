package clients

import (
	"google.golang.org/grpc/codes"

	worldv1 "finga/api/gen/world/v1"
	"finga/pkg/grpcx"
	"finga/services/gateway/internal/clients/errmap"
	"finga/services/gateway/internal/model"
)

// worldPush turns a world stream event into its push; false for an unknown event.
func worldPush(m *worldv1.DeliverResponse) (model.Push, bool) {
	switch {
	case m.GetSnapshot() != nil:
		ev := m.GetSnapshot()
		return model.Snapshot{Players: playerViews(ev.GetPlayers()), Mobs: mobViews(ev.GetMobs()), Drops: dropViews(ev.GetDrops())}, true
	case m.GetTeleport() != nil:
		ev := m.GetTeleport()
		return model.MapChange{MapID: ev.GetMapId(), X: ev.GetX(), Y: ev.GetY()}, true
	case m.GetDamage() != nil:
		ev := m.GetDamage()
		return model.Damage{Source: entity(ev.GetSource()), Target: entity(ev.GetTarget()), Amount: int(ev.GetAmount()), Crit: ev.GetCrit(), Miss: ev.GetMiss()}, true
	case m.GetDied() != nil:
		ev := m.GetDied()
		return model.Died{CharacterID: ev.GetCharacterId(), X: ev.GetX(), Y: ev.GetY()}, true
	case m.GetRespawned() != nil:
		ev := m.GetRespawned()
		return model.Respawned{CharacterID: ev.GetCharacterId(), MapID: ev.GetMapId(), X: ev.GetX(), Y: ev.GetY(), HP: int(ev.GetHp())}, true
	case m.GetExpGained() != nil:
		ev := m.GetExpGained()
		return model.ExpGained{Amount: ev.GetAmount(), Exp: ev.GetExp()}, true
	case m.GetLevelUp() != nil:
		return model.LevelUp{Character: characterView(m.GetLevelUp().GetCharacter())}, true
	case m.GetChat() != nil:
		ev := m.GetChat()
		return model.ChatMessage{Channel: model.ChatMap, Sender: ev.GetSenderId(), SenderName: ev.GetSenderName(), Text: ev.GetText()}, true
	case m.GetInventoryChanged() != nil:
		return model.InventoryChanged{Revision: m.GetInventoryChanged().GetRevision(), Slots: worldSlots(m.GetInventoryChanged().GetSlots())}, true
	case m.GetPickupFailed() != nil:
		return model.PickupFailed{Why: pickupFailure(m.GetPickupFailed())}, true
	}
	return nil, false
}

// frameOf is a Frame's Delta, which every recipient shares.
func frameOf(d *worldv1.Delta) *model.Frame {
	return &model.Frame{
		Players: playerViews(d.GetPlayers()), Mobs: mobViews(d.GetMobs()), Drops: dropViews(d.GetDrops()),
		RemovedPlayers: d.GetRemovedPlayers(), RemovedMobs: d.GetRemovedMobs(), RemovedDrops: d.GetRemovedDrops(),
	}
}

// pickupFailure is a failed pickup as the player sees it; one without a reason is internal.
func pickupFailure(f *worldv1.PickupFailed) model.Public {
	if f.GetErrorReason() == "" {
		return model.Internal
	}
	return errmap.For(grpcx.ErrorFor(codes.Unknown, grpcx.Reason{Domain: f.GetErrorDomain(), Name: f.GetErrorReason()}))
}

func entity(r *worldv1.EntityRef) model.Entity {
	return model.Entity{Character: r.GetCharacterId(), Mob: r.GetMobId()}
}

func characterView(c *worldv1.CharacterView) model.CharacterView {
	st := c.GetStats()
	return model.CharacterView{
		ID: c.GetId(), Name: c.GetName(), Class: c.GetClass(), Level: int(c.GetLevel()), Exp: c.GetExp(),
		Stats: model.Stats{STR: int(st.GetStrength()), DEX: int(st.GetDexterity()), INT: int(st.GetIntelligence()), AGI: int(st.GetAgility()), VIT: int(st.GetVitality())},
		HP:    int(c.GetHp()), MaxHP: int(c.GetMaxHp()), MP: int(c.GetMp()), MaxMP: int(c.GetMaxMp()),
	}
}

func playerViews(ps []*worldv1.PlayerView) []model.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]model.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, model.PlayerView{
			ID: p.GetId(), Name: p.GetName(), Class: p.GetClass(), Level: int(p.GetLevel()),
			X: p.GetX(), Y: p.GetY(), VX: p.GetVx(), VY: p.GetVy(), Facing: int(p.GetFacing()),
			OnGround: p.GetOnGround(), Climbing: p.GetClimbing(), HP: int(p.GetHp()), MaxHP: int(p.GetMaxHp()), Dead: p.GetDead(),
			Invulnerable: p.GetInvulnerable(),
		})
	}
	return out
}

func mobViews(ms []*worldv1.MobView) []model.MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]model.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, model.MobView{ID: m.GetId(), Type: m.GetType(), X: m.GetX(), Y: m.GetY(), Facing: int(m.GetFacing()), HP: int(m.GetHp()), MaxHP: int(m.GetMaxHp())})
	}
	return out
}

func dropViews(ds []*worldv1.DropView) []model.DropView {
	if len(ds) == 0 {
		return nil
	}
	out := make([]model.DropView, 0, len(ds))
	for _, d := range ds {
		out = append(out, model.DropView{ID: d.GetId(), Template: d.GetTemplate(), Quantity: int(d.GetQuantity()), X: d.GetX(), Y: d.GetY()})
	}
	return out
}

// worldSlots are the slots a pickup changed, as the world's contract carries them.
func worldSlots(slots []*worldv1.Slot) []model.InventorySlot {
	out := make([]model.InventorySlot, 0, len(slots))
	for _, s := range slots {
		slot := model.InventorySlot{Tab: s.GetTab(), Slot: int(s.GetSlot())}
		if it := s.GetItem(); it != nil {
			slot.Item = &model.ItemView{ID: it.GetId(), Template: it.GetTemplateId(), Tab: it.GetTab(), Slot: int(it.GetSlot()), Quantity: int(it.GetQuantity())}
		}
		out = append(out, slot)
	}
	return out
}
