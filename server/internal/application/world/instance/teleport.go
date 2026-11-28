package instance

import (
	"context"
	"fmt"
	"slices"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
	"finga/pkg/transient"
)

// teleportMsg asks to start a teleport channel to a waypoint or, with none, to one of the old maps; done hears whether it started.
type teleportMsg struct {
	id       kernel.CharacterID
	conn     string
	mapID    string
	waypoint string
	seq      uint32 // the input it was pressed during
	done     chan error
}

func (teleportMsg) message() {}

// switchMsg asks to start a switch to the region's channel n.
type switchMsg struct {
	id   kernel.CharacterID
	conn string
	n    int
	seq  uint32
	done chan error
}

func (switchMsg) message() {}

// channel is a teleport or a channel switch under way: where to, the input it was pressed during, and the tick it takes the player.
type channel struct {
	target *gamedata.Map
	at     *gamedata.Waypoint // a teleport to a waypoint's, beside which it lands; nil for an old map's spawn
	to     int                // a switch's channel of this region; 0 on a teleport
	seq    uint32
	end    uint64
}

// Teleport starts a teleport channel to the waypoint, or, with none, to the old map, pressed during input seq, and returns once it has started or been refused.
func (i *Instance) Teleport(ctx context.Context, id kernel.CharacterID, conn, mapID, waypoint string, seq uint32) error {
	m := teleportMsg{id: id, conn: conn, mapID: mapID, waypoint: waypoint, seq: seq, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// startTeleport starts the player's channel, which ends at the data's channel length unless something cancels it first.
func (i *Instance) startTeleport(m teleportMsg) {
	p, ok := i.players[m.id]
	if !ok || p.Connection != m.conn {
		m.done <- fmt.Errorf("%w: the character is not on this map through that connection", transient.ErrUnavailable)
		return
	}
	target, at, err := i.teleportTarget(p, m)
	switch {
	case err != nil:
		m.done <- err
	case p.dead:
		m.done <- model.ErrTeleportDead
	case p.channel != nil:
		m.done <- model.ErrTeleportBusy
	default:
		p.channel = &channel{target: target, at: at, seq: p.pressedDuring(m.seq), end: i.tick + i.ticksFor(i.data.TeleportChannel())}
		m.done <- nil
	}
}

// teleportTarget is where a teleport goes (large-regions.md section 8): a waypoint the player has found, or one in its region's main town, in any region.
func (i *Instance) teleportTarget(p *player, m teleportMsg) (*gamedata.Map, *gamedata.Waypoint, error) {
	if m.waypoint != "" {
		target, w, ok := i.data.Waypoint(m.waypoint)
		if !ok {
			return nil, nil, model.ErrTeleportUnknownWaypoint
		}
		if _, found := p.Waypoints[w.ID]; !found && !w.Main {
			return nil, nil, model.ErrTeleportNotFound
		}
		return target, &w, nil
	}
	return nil, nil, model.ErrTeleportUnknownMap // a match's map is reached by a match alone, a region by its waypoints
}

// SwitchChannel starts a switch to the region's channel n, pressed during input seq, and returns once it has started or been refused; the World has checked n is open with room.
func (i *Instance) SwitchChannel(ctx context.Context, id kernel.CharacterID, conn string, n int, seq uint32) error {
	m := switchMsg{id: id, conn: conn, n: n, seq: seq, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// startSwitch starts the player's switch, a teleport's channel to the same spot in channel n.
func (i *Instance) startSwitch(m switchMsg) {
	p, ok := i.players[m.id]
	switch {
	case !ok || p.Connection != m.conn:
		m.done <- fmt.Errorf("%w: the character is not on this map through that connection", transient.ErrUnavailable)
	case p.dead:
		m.done <- model.ErrTeleportDead
	case p.channel != nil:
		m.done <- model.ErrTeleportBusy
	default:
		p.channel = &channel{target: i.m, to: m.n, seq: p.pressedDuring(m.seq), end: i.tick + i.ticksFor(i.data.TeleportChannel())}
		m.done <- nil
	}
}

// cancelTeleport ends the player's channel, if one runs, and tells them why.
func (i *Instance) cancelTeleport(p *player, why model.TeleportCancel) {
	if p.channel == nil {
		return
	}
	p.channel = nil
	p.private = append(p.private, model.TeleportCancelled{Tick: i.tick, Reason: why})
}

// movedDuring cancels the player's channel for an input pressed after it with a movement or jump key held.
func (i *Instance) movedDuring(p *player, seq uint32, in sim.Input) {
	if c := p.channel; c != nil && seq > c.seq && moving(in) {
		i.cancelTeleport(p, model.TeleportMoved)
	}
}

// movedOn cancels the player's channel when a gap in their inputs, past the hold, steps them on with a movement or jump key still held.
func (i *Instance) movedOn(p *player) {
	if p.channel != nil && moving(p.held) {
		i.cancelTeleport(p, model.TeleportMoved)
	}
}

func moving(in sim.Input) bool { return in.Left || in.Right || in.Up || in.Down || in.Jump }

// teleports hands off each player whose channel ends this tick: a teleport's beside its waypoint, or to its old map's player spawn, a switch's to where they stand in the other channel; an item call in flight settles here first.
func (i *Instance) teleports() {
	for _, id := range slices.Clone(i.order) {
		p := i.players[id]
		c := p.channel
		if c == nil || c.end > i.tick || p.itemCalls > 0 {
			continue
		}
		p.channel = nil
		if c.to > 0 {
			moved := p.Player
			moved.Timers = i.timersOf(p, time.Now())
			i.leave(p, i.m, p.state.X, p.state.Y, p.state.Z, facing(p.state), moved, Dest{ID: i.m.ID, Channel: c.to, Switch: true})
			continue
		}
		if w := c.at; w != nil {
			dest := Dest{ID: c.target.ID}
			if c.target.ID == i.m.ID {
				dest.Channel, dest.Switch = i.channel, true // in the player's own region, back into their own channel, even if it filled meanwhile
			}
			i.handOffTo(p, c.target, w.Arrive.X, w.Arrive.Z, float32(w.Yaw), dest)
			continue
		}
		i.handOff(p, c.target, c.target.PlayerSpawn.X, c.target.PlayerSpawn.Z, float32(c.target.SpawnYaw))
	}
}
