package instance

import (
	"context"
	"fmt"
	"slices"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
	"finga/pkg/transient"
)

// teleportMsg asks to start a teleport channel to a map; done hears whether it started.
type teleportMsg struct {
	id    kernel.CharacterID
	conn  string
	mapID string
	seq   uint32 // the input it was pressed during
	done  chan error
}

func (teleportMsg) message() {}

// channel is a teleport under way: where to, the input it was pressed during, and the tick it takes the player.
type channel struct {
	target *gamedata.Map
	seq    uint32
	end    uint64
}

// Teleport starts a teleport channel to the map, pressed during input seq, and returns once it has started or been refused.
func (i *Instance) Teleport(ctx context.Context, id kernel.CharacterID, conn, mapID string, seq uint32) error {
	m := teleportMsg{id: id, conn: conn, mapID: mapID, seq: seq, done: make(chan error, 1)}
	return i.ask(ctx, m, m.done)
}

// startTeleport starts the player's channel, which ends at the data's channel length unless something cancels it first.
func (i *Instance) startTeleport(m teleportMsg) {
	p, ok := i.players[m.id]
	if !ok || p.Connection != m.conn {
		m.done <- fmt.Errorf("%w: the character is not on this map through that connection", transient.ErrUnavailable)
		return
	}
	target, ok := i.data.Map(m.mapID)
	switch {
	case !ok:
		m.done <- model.ErrTeleportUnknownMap
	case target.ID == i.m.ID:
		m.done <- model.ErrTeleportSameMap
	case p.dead:
		m.done <- model.ErrTeleportDead
	case p.channel != nil:
		m.done <- model.ErrTeleportBusy
	default:
		p.channel = &channel{target: target, seq: p.pressedDuring(m.seq), end: i.tick + i.ticksFor(i.data.TeleportChannel())}
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

// teleports hands off each player whose channel ends this tick to its map's player spawn, and returns those handed off; an item call in flight settles here first.
func (i *Instance) teleports() []kernel.CharacterID {
	var left []kernel.CharacterID
	for _, id := range slices.Clone(i.order) {
		p := i.players[id]
		c := p.channel
		if c == nil || c.end > i.tick || p.itemCalls > 0 {
			continue
		}
		p.channel = nil
		i.handOff(p, c.target, c.target.PlayerSpawn.X, c.target.PlayerSpawn.Z, float32(c.target.SpawnYaw))
		left = append(left, id)
	}
	return left
}
