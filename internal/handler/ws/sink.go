package ws

import (
	"finga/internal/application/world"
	"finga/internal/protocol"
	"finga/pkg/wsconn"
)

// sink delivers a map instance's events to one connection, translated to
// protocol messages. Send never blocks: a full queue closes the connection
// and reports false, and the instance drops the player.
type sink struct {
	conn wsconn.Conn
	obs  Observer
}

// Send translates one world event and enqueues it on the connection.
func (s *sink) Send(e world.Event) bool {
	var m wsconn.Message
	switch ev := e.(type) {
	case world.Snapshot:
		m = wsconn.Message{Type: "Snapshot", Tick: ev.Tick, Data: protocol.Snapshot{
			Players: playerViews(ev.Players), Mobs: []protocol.MobView{}, Drops: []protocol.DropView{},
		}}
	case world.Delta:
		d := protocol.Delta{AckSeq: ev.AckSeq, Players: playerViews(ev.Players)}
		for _, id := range ev.Removed {
			d.RemovedPlayers = append(d.RemovedPlayers, id.String())
		}
		m = wsconn.Message{Type: "Delta", Tick: ev.Tick, Data: d}
	case world.Teleport:
		m = wsconn.Message{Type: "MapChange", Tick: ev.Tick, Data: protocol.MapChange{MapID: ev.MapID, X: ev.X, Y: ev.Y}}
	default:
		return true // an event this gateway does not know yet; not a reason to drop the player
	}
	s.obs.MessageOut(m.Type)
	return s.conn.Send(m) == nil
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
			OnGround: p.State.OnGround, Climbing: p.State.Climbing, HP: p.HP, MaxHP: p.MaxHP,
		})
	}
	return out
}
