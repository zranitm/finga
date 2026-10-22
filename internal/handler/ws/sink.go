package ws

import (
	"strconv"

	"finga/internal/application/world"
	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

// sink delivers a map instance's events to one connection, translated to
// protocol messages. Send never blocks: a full queue closes the connection
// and reports false, and the instance drops the player. A Teleport also
// re-points the session at the target instance, so inputs follow.
type sink struct {
	conn       wsconn.Conn
	obs        Observer
	onTeleport func(mapID string)
}

// Send translates one world event and enqueues it on the connection.
func (s *sink) Send(e world.Event) bool {
	var m wsconn.Message
	switch ev := e.(type) {
	case world.Snapshot:
		m = wsconn.Message{Type: "Snapshot", Tick: ev.Tick, Data: protocol.Snapshot{
			Players: playerViews(ev.Players), Mobs: mobViews(ev.Mobs, true), Drops: dropViews(ev.Drops, true),
		}}
	case world.Delta:
		d := protocol.Delta{AckSeq: ev.AckSeq, Players: playerViews(ev.Players), Mobs: mobViews(ev.Mobs, false), Drops: dropViews(ev.Drops, false), RemovedMobs: ev.RemovedMobs, RemovedDrops: ev.RemovedDrops}
		for _, id := range ev.Removed {
			d.RemovedPlayers = append(d.RemovedPlayers, id.String())
		}
		m = wsconn.Message{Type: "Delta", Tick: ev.Tick, Data: d}
	case world.Teleport:
		if s.onTeleport != nil {
			s.onTeleport(ev.MapID)
		}
		m = wsconn.Message{Type: "MapChange", Tick: ev.Tick, Data: protocol.MapChange{MapID: ev.MapID, X: ev.X, Y: ev.Y}}
	case world.Damage:
		m = wsconn.Message{Type: "Damage", Tick: ev.Tick, Data: protocol.Damage{Source: entityRef(ev.Source), Target: entityRef(ev.Target), Amount: ev.Amount, Crit: ev.Crit, Miss: ev.Miss}}
	case world.Died:
		m = wsconn.Message{Type: "Died", Tick: ev.Tick, Data: protocol.Died{CharacterID: ev.ID.String(), X: ev.X, Y: ev.Y}}
	case world.Respawned:
		m = wsconn.Message{Type: "Respawned", Tick: ev.Tick, Data: protocol.Respawned{CharacterID: ev.ID.String(), MapID: ev.MapID, X: ev.X, Y: ev.Y, HP: ev.HP}}
	case world.ExpGained:
		m = wsconn.Message{Type: "ExpGained", Tick: ev.Tick, Data: protocol.ExpGained{Amount: ev.Amount, Exp: ev.Exp}}
	case world.LevelUp:
		m = wsconn.Message{Type: "LevelUp", Tick: ev.Tick, Data: protocol.LevelUp{Character: protocol.CharacterView{
			ID: ev.ID.String(), Name: ev.Name, Class: ev.Class.String(), Level: ev.Level, Exp: ev.Exp,
			Stats: protocol.Stats{STR: ev.Stats.STR, DEX: ev.Stats.DEX, INT: ev.Stats.INT, AGI: ev.Stats.AGI, VIT: ev.Stats.VIT},
			HP:    ev.HP, MaxHP: ev.MaxHP, MP: ev.MP, MaxMP: ev.MaxMP,
		}}}
	case world.Chat:
		m = wsconn.Message{Type: "ChatMessage", Tick: ev.Tick, Data: protocol.ChatMessage{Channel: protocol.ChatMap, Sender: ev.Sender.String(), SenderName: ev.SenderName, Text: ev.Text}}
	case world.InventoryChanged:
		m = wsconn.Message{Type: "InventoryChanged", Tick: ev.Tick, Data: protocol.InventoryChanged{Slots: inventorySlots(ev.Slots)}}
	case world.PickupFailed:
		m = wsconn.Message{Type: "Response", Tick: ev.Tick, Data: protocol.Response{OK: false, Code: ev.Code, Message: ev.Message}}
	default:
		return true // an event this gateway does not know yet; not a reason to drop the player
	}
	s.obs.MessageOut(m.Type)
	return s.conn.Send(m) == nil
}

// entityRef is page 4's wire form: p:<character id> or m:<mob id>.
func entityRef(r world.EntityRef) string {
	if r.IsMob() {
		return "m:" + strconv.FormatUint(r.Mob, 10)
	}
	return "p:" + r.Player.String()
}

func playerViews(ps []world.PlayerView) []protocol.PlayerView {
	if len(ps) == 0 {
		return nil
	}
	out := make([]protocol.PlayerView, 0, len(ps))
	for _, p := range ps {
		out = append(out, protocol.PlayerView{
			ID: p.ID.String(), Name: p.Name, Class: p.Class.String(), Level: p.Level,
			X: p.State.X, Y: p.State.Y, VX: p.State.VX, VY: p.State.VY, Facing: p.State.Facing,
			OnGround: p.State.OnGround, Climbing: p.State.Climbing, HP: p.HP, MaxHP: p.MaxHP, Dead: p.Dead,
			Invulnerable: p.Invulnerable,
		})
	}
	return out
}

// mobViews converts mobs; a snapshot always carries an array, a delta
// omits an empty one.
func mobViews(ms []world.MobView, always bool) []protocol.MobView {
	if len(ms) == 0 {
		if always {
			return []protocol.MobView{}
		}
		return nil
	}
	out := make([]protocol.MobView, 0, len(ms))
	for _, m := range ms {
		out = append(out, protocol.MobView{ID: m.ID, Type: m.Type, X: m.X, Y: m.Y, Facing: m.Facing, HP: m.HP, MaxHP: m.MaxHP})
	}
	return out
}

// dropViews converts ground drops; a snapshot always carries an array.
func dropViews(ds []world.DropView, always bool) []protocol.DropView {
	if len(ds) == 0 {
		if always {
			return []protocol.DropView{}
		}
		return nil
	}
	out := make([]protocol.DropView, 0, len(ds))
	for _, d := range ds {
		out = append(out, protocol.DropView{ID: d.ID, Template: d.Template, Quantity: d.Quantity, X: d.X, Y: d.Y})
	}
	return out
}
