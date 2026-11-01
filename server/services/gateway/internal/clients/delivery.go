package clients

import (
	"strconv"

	"google.golang.org/grpc/codes"

	clientv1 "finga/api/gen/client/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/pkg/grpcx"
	"finga/services/gateway/internal/clients/errmap"
	"finga/services/gateway/internal/model"
)

// delivery turns a world stream event into its protocol message; false for an unknown event.
func delivery(m *worldv1.DeliverResponse) (model.Delivery, bool) {
	tick := m.GetTick()
	switch {
	case m.GetSnapshot() != nil:
		ev := m.GetSnapshot()
		return model.Delivery{Type: model.DeliverySnapshot, Tick: tick, Data: &clientv1.Snapshot{
			Players: playerViews(ev.GetPlayers()), Mobs: mobViews(ev.GetMobs()), Drops: dropViews(ev.GetDrops()),
		}}, true
	case m.GetDelta() != nil:
		ev := m.GetDelta()
		return model.Delivery{Type: "Delta", Tick: tick, Data: &clientv1.Delta{
			AckSeq: ev.GetAckSeq(), Players: playerViews(ev.GetPlayers()), Mobs: mobViews(ev.GetMobs()), Drops: dropViews(ev.GetDrops()),
			RemovedPlayers: ev.GetRemovedPlayers(), RemovedMobs: instanceIDs(ev.GetRemovedMobs()), RemovedDrops: instanceIDs(ev.GetRemovedDrops()),
		}}, true
	case m.GetTeleport() != nil:
		ev := m.GetTeleport()
		return model.Delivery{Type: "MapChange", Tick: tick, MapID: ev.GetMapId(), Data: &clientv1.MapChange{MapId: ev.GetMapId(), X: ev.GetX(), Y: ev.GetY()}}, true
	case m.GetDamage() != nil:
		ev := m.GetDamage()
		return model.Delivery{Type: "Damage", Tick: tick, Data: &clientv1.Damage{
			Source: entityRef(ev.GetSource()), Target: entityRef(ev.GetTarget()), Amount: ev.GetAmount(), Crit: ev.GetCrit(), Miss: ev.GetMiss(),
		}}, true
	case m.GetDied() != nil:
		ev := m.GetDied()
		return model.Delivery{Type: "Died", Tick: tick, Data: &clientv1.Died{CharacterId: ev.GetCharacterId(), X: ev.GetX(), Y: ev.GetY()}}, true
	case m.GetRespawned() != nil:
		ev := m.GetRespawned()
		return model.Delivery{Type: "Respawned", Tick: tick, Data: &clientv1.Respawned{CharacterId: ev.GetCharacterId(), MapId: ev.GetMapId(), X: ev.GetX(), Y: ev.GetY(), Hp: ev.GetHp()}}, true
	case m.GetExpGained() != nil:
		ev := m.GetExpGained()
		return model.Delivery{Type: "ExpGained", Tick: tick, Data: &clientv1.ExpGained{Amount: ev.GetAmount(), Exp: ev.GetExp()}}, true
	case m.GetLevelUp() != nil:
		return model.Delivery{Type: "LevelUp", Tick: tick, Data: &clientv1.LevelUp{Character: characterView(m.GetLevelUp().GetCharacter())}}, true
	case m.GetChat() != nil:
		ev := m.GetChat()
		return model.Delivery{Type: "ChatMessage", Tick: tick, Data: &clientv1.ChatMessage{
			Channel: clientv1.ChatChannel_CHAT_CHANNEL_MAP, Sender: ev.GetSenderId(), SenderName: ev.GetSenderName(), Text: ev.GetText(),
		}}, true
	case m.GetInventoryChanged() != nil:
		return model.Delivery{Type: "InventoryChanged", Tick: tick, Data: &clientv1.InventoryChanged{Slots: inventorySlots(m.GetInventoryChanged().GetSlots())}}, true
	case m.GetPickupFailed() != nil:
		return model.Delivery{Type: "Response", Tick: tick, Data: refusal(pickupFailure(m.GetPickupFailed()))}, true
	}
	return model.Delivery{}, false
}

// pickupFailure is a failed pickup as the player sees it; one without a reason is internal.
func pickupFailure(f *worldv1.PickupFailed) model.Public {
	if f.GetErrorReason() == "" {
		return model.Internal
	}
	return errmap.For(grpcx.ErrorFor(codes.Unknown, grpcx.Reason{Domain: f.GetErrorDomain(), Name: f.GetErrorReason()}))
}

// entityRef is the wire form p:<character id> or m:<mob id>.
func entityRef(r *worldv1.EntityRef) string {
	if id := r.GetCharacterId(); id != "" {
		return "p:" + id
	}
	return "m:" + strconv.FormatUint(r.GetMobId(), 10)
}

func characterView(c *worldv1.CharacterView) *clientv1.CharacterView {
	st := c.GetStats()
	return &clientv1.CharacterView{
		Id: c.GetId(), Name: c.GetName(), Class: c.GetClass(), Level: c.GetLevel(), Exp: c.GetExp(),
		Stats: &clientv1.Stats{Str: st.GetStrength(), Dex: st.GetDexterity(), Int: st.GetIntelligence(), Agi: st.GetAgility(), Vit: st.GetVitality()},
		Hp:    c.GetHp(), MaxHp: c.GetMaxHp(), Mp: c.GetMp(), MaxMp: c.GetMaxMp(),
	}
}

func playerViews(ps []*worldv1.PlayerView) []*clientv1.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]*clientv1.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, &clientv1.PlayerView{
			Id: p.GetId(), Name: p.GetName(), Class: p.GetClass(), Level: p.GetLevel(),
			X: p.GetX(), Y: p.GetY(), Vx: p.GetVx(), Vy: p.GetVy(), Facing: p.GetFacing(),
			OnGround: p.GetOnGround(), Climbing: p.GetClimbing(), Hp: p.GetHp(), MaxHp: p.GetMaxHp(), Dead: p.GetDead(),
			Invulnerable: p.GetInvulnerable(),
		})
	}
	return out
}

func mobViews(ms []*worldv1.MobView) []*clientv1.MobView {
	if len(ms) == 0 {
		return nil
	}
	out := make([]*clientv1.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, &clientv1.MobView{Id: uint32(m.GetId()), Type: m.GetType(), X: m.GetX(), Y: m.GetY(), Facing: m.GetFacing(), Hp: m.GetHp(), MaxHp: m.GetMaxHp()})
	}
	return out
}

func dropViews(ds []*worldv1.DropView) []*clientv1.DropView {
	if len(ds) == 0 {
		return nil
	}
	out := make([]*clientv1.DropView, 0, len(ds))
	for _, d := range ds {
		out = append(out, &clientv1.DropView{Id: uint32(d.GetId()), Template: d.GetTemplate(), Quantity: d.GetQuantity(), X: d.GetX(), Y: d.GetY()})
	}
	return out
}

// instanceIDs are mob or drop IDs, which count up within one map instance and fit the wire's 32 bits.
func instanceIDs(ids []uint64) []uint32 {
	if len(ids) == 0 {
		return nil
	}
	out := make([]uint32, 0, len(ids))
	for _, id := range ids {
		out = append(out, uint32(id))
	}
	return out
}

func refusal(p model.Public) *clientv1.Response {
	return &clientv1.Response{Ok: false, Code: p.Code, Message: p.Message}
}
