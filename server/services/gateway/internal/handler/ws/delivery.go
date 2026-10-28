package ws

import (
	"strconv"

	"google.golang.org/grpc/codes"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/protocol"
	"finga/pkg/grpcx"
	"finga/pkg/wsconn"
	"finga/services/gateway/internal/handler/errmap"
)

// delivery turns one event of the world's stream into the protocol message
// the player sees; false for an event this gateway does not know yet.
func delivery(m *worldv1.DeliverResponse) (wsconn.Message, bool) {
	tick := m.GetTick()
	switch {
	case m.GetSnapshot() != nil:
		ev := m.GetSnapshot()
		return wsconn.Message{Type: "Snapshot", Tick: tick, Data: protocol.Snapshot{
			Players: playerViews(ev.GetPlayers()), Mobs: mobViews(ev.GetMobs(), true), Drops: dropViews(ev.GetDrops(), true),
		}}, true
	case m.GetDelta() != nil:
		ev := m.GetDelta()
		return wsconn.Message{Type: "Delta", Tick: tick, Data: protocol.Delta{
			AckSeq: ev.GetAckSeq(), Players: playerViews(ev.GetPlayers()), Mobs: mobViews(ev.GetMobs(), false), Drops: dropViews(ev.GetDrops(), false),
			RemovedPlayers: ev.GetRemovedPlayers(), RemovedMobs: ev.GetRemovedMobs(), RemovedDrops: ev.GetRemovedDrops(),
		}}, true
	case m.GetTeleport() != nil:
		ev := m.GetTeleport()
		return wsconn.Message{Type: "MapChange", Tick: tick, Data: protocol.MapChange{MapID: ev.GetMapId(), X: ev.GetX(), Y: ev.GetY()}}, true
	case m.GetDamage() != nil:
		ev := m.GetDamage()
		return wsconn.Message{Type: "Damage", Tick: tick, Data: protocol.Damage{
			Source: entityRef(ev.GetSource()), Target: entityRef(ev.GetTarget()), Amount: int(ev.GetAmount()), Crit: ev.GetCrit(), Miss: ev.GetMiss(),
		}}, true
	case m.GetDied() != nil:
		ev := m.GetDied()
		return wsconn.Message{Type: "Died", Tick: tick, Data: protocol.Died{CharacterID: ev.GetCharacterId(), X: ev.GetX(), Y: ev.GetY()}}, true
	case m.GetRespawned() != nil:
		ev := m.GetRespawned()
		return wsconn.Message{Type: "Respawned", Tick: tick, Data: protocol.Respawned{CharacterID: ev.GetCharacterId(), MapID: ev.GetMapId(), X: ev.GetX(), Y: ev.GetY(), HP: int(ev.GetHp())}}, true
	case m.GetExpGained() != nil:
		ev := m.GetExpGained()
		return wsconn.Message{Type: "ExpGained", Tick: tick, Data: protocol.ExpGained{Amount: ev.GetAmount(), Exp: ev.GetExp()}}, true
	case m.GetLevelUp() != nil:
		return wsconn.Message{Type: "LevelUp", Tick: tick, Data: protocol.LevelUp{Character: characterView(m.GetLevelUp().GetCharacter())}}, true
	case m.GetChat() != nil:
		ev := m.GetChat()
		return wsconn.Message{Type: "ChatMessage", Tick: tick, Data: protocol.ChatMessage{Channel: protocol.ChatMap, Sender: ev.GetSenderId(), SenderName: ev.GetSenderName(), Text: ev.GetText()}}, true
	case m.GetInventoryChanged() != nil:
		return wsconn.Message{Type: "InventoryChanged", Tick: tick, Data: protocol.InventoryChanged{Slots: inventorySlots(m.GetInventoryChanged().GetSlots())}}, true
	case m.GetPickupFailed() != nil:
		return wsconn.Message{Type: "Response", Tick: tick, Data: refusal(pickupFailure(m.GetPickupFailed()))}, true
	}
	return wsconn.Message{}, false
}

// pickupFailure is a failed pickup as the player sees it, by the reason
// the world named; one without a reason was logged by the world, where its
// cause is known, and is internal here.
func pickupFailure(f *worldv1.PickupFailed) errmap.Public {
	if f.GetErrorReason() == "" {
		return errmap.Internal
	}
	return errmap.For(grpcx.ErrorFor(codes.Unknown, grpcx.Reason{Domain: f.GetErrorDomain(), Name: f.GetErrorReason()}))
}

// entityRef is page 4's wire form: p:<character id> or m:<mob id>.
func entityRef(r *worldv1.EntityRef) string {
	if id := r.GetCharacterId(); id != "" {
		return "p:" + id
	}
	return "m:" + strconv.FormatUint(r.GetMobId(), 10)
}

func characterView(c *worldv1.CharacterView) protocol.CharacterView {
	st := c.GetStats()
	return protocol.CharacterView{
		ID: c.GetId(), Name: c.GetName(), Class: c.GetClass(), Level: int(c.GetLevel()), Exp: c.GetExp(),
		Stats: protocol.Stats{STR: int(st.GetStrength()), DEX: int(st.GetDexterity()), INT: int(st.GetIntelligence()), AGI: int(st.GetAgility()), VIT: int(st.GetVitality())},
		HP:    int(c.GetHp()), MaxHP: int(c.GetMaxHp()), MP: int(c.GetMp()), MaxMP: int(c.GetMaxMp()),
	}
}

func playerViews(ps []*worldv1.PlayerView) []protocol.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]protocol.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, protocol.PlayerView{
			ID: p.GetId(), Name: p.GetName(), Class: p.GetClass(), Level: int(p.GetLevel()),
			X: p.GetX(), Y: p.GetY(), VX: p.GetVx(), VY: p.GetVy(), Facing: int(p.GetFacing()),
			OnGround: p.GetOnGround(), Climbing: p.GetClimbing(), HP: int(p.GetHp()), MaxHP: int(p.GetMaxHp()), Dead: p.GetDead(),
			Invulnerable: p.GetInvulnerable(),
		})
	}
	return out
}

// mobViews converts mobs; a snapshot always carries an array, a delta
// omits an empty one.
func mobViews(ms []*worldv1.MobView, always bool) []protocol.MobView {
	if len(ms) == 0 {
		if always {
			return []protocol.MobView{}
		}
		return nil
	}
	out := make([]protocol.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, protocol.MobView{ID: m.GetId(), Type: m.GetType(), X: m.GetX(), Y: m.GetY(), Facing: int(m.GetFacing()), HP: int(m.GetHp()), MaxHP: int(m.GetMaxHp())})
	}
	return out
}

// dropViews converts ground drops; a snapshot always carries an array.
func dropViews(ds []*worldv1.DropView, always bool) []protocol.DropView {
	if len(ds) == 0 {
		if always {
			return []protocol.DropView{}
		}
		return nil
	}
	out := make([]protocol.DropView, 0, len(ds))
	for _, d := range ds {
		out = append(out, protocol.DropView{ID: d.GetId(), Template: d.GetTemplate(), Quantity: int(d.GetQuantity()), X: d.GetX(), Y: d.GetY()})
	}
	return out
}
