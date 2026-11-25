package instance

import (
	"context"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/kernel"
	"finga/internal/shared/sim"
)

// rejoined is a rejoin's answer: the view its Welcome shows, or why the match didn't take it.
type rejoined struct {
	view model.View
	err  error
}

// Hold keeps a match fighter's place and body through conn's drop for the mode's reconnect, with no bot standing in (pvp.md section 4); false off a match, or for a fighter the match can't hold, whom the caller takes off as any leave.
func (i *Instance) Hold(ctx context.Context, id kernel.CharacterID, conn string) (bool, error) {
	m := holdMsg{id: id, conn: conn, done: make(chan bool, 1)}
	return answer(ctx, i, m, m.done)
}

// Rejoin puts conn and its sink on the character's body in this match, its place held or still connected, and answers the view its Welcome shows; ErrNotInMatch when the match doesn't have them, for a plain join.
func (i *Instance) Rejoin(ctx context.Context, id kernel.CharacterID, conn string, sink Sink) (model.View, error) {
	m := rejoinMsg{id: id, conn: conn, sink: sink, done: make(chan rejoined, 1)}
	r, err := answer(ctx, i, m, m.done)
	if err != nil {
		return model.View{}, err
	}
	return r.view, r.err
}

// answer posts m and waits for what the tick sends on done, ctx's end, or the map stopping.
func answer[T any](ctx context.Context, i *Instance, m message, done <-chan T) (T, error) {
	var zero T
	if err := i.post(m); err != nil {
		return zero, err
	}
	select {
	case v := <-done:
		return v, nil
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-i.stopped:
		return zero, model.ErrStopped
	}
}

// fighterOf is id's place in the match while they're on its map before the outcome; nil otherwise.
func (i *Instance) fighterOf(id kernel.CharacterID) (*player, *fighter) {
	p, ok := i.players[id]
	if i.match == nil || !ok || i.match.phase == model.PhaseResult {
		return nil, nil
	}
	return p, i.match.fighters[id]
}

// hold keeps the place of the fighter connected through conn: their body stays and is the player's, its timers running and open to hits and resets, but sent nothing and stepping on no keys till they're back or the grace ends, its attack and skill still to come dropped. A grace already running keeps its end, so a return that fails buys no time.
func (i *Instance) hold(id kernel.CharacterID, conn string) bool {
	p, f := i.fighterOf(id)
	if p == nil || p.Connection != conn || i.match.Mode.Reconnect <= 0 {
		return false
	}
	if f.away == 0 {
		f.away = i.tick + i.ticksFor(i.match.Mode.Reconnect)
	}
	p.sink = botSink{}
	p.held, p.pending = sim.Input{}, nil
	p.windUps, p.attackQueued = nil, false // a hit still to land and an attack still to start go with the connection
	if m := p.waitingCast; m != nil {
		m.done <- ErrSinkClosed
		p.waitingCast = nil
	}
	i.match.tell = true
	return true
}

// away reports whether id's place is held through a drop, so their keys, any still on their way included, step nothing.
func (i *Instance) away(id kernel.CharacterID) bool {
	if i.match == nil {
		return false
	}
	f := i.match.fighters[id]
	return f != nil && f.away != 0
}

// rejoin puts conn and sink on the fighter's body as it stands: the inputs count from the new connection's, the map, the fighter's sheet and timers and the arena bar they wear are told afresh, and the match hears they're back.
func (i *Instance) rejoin(id kernel.CharacterID, conn string, sink Sink) (model.View, error) {
	p, f := i.fighterOf(id)
	if p == nil {
		return model.View{}, model.ErrNotInMatch
	}
	p.Connection, p.sink, p.joined = conn, sink, true
	// The inputs, and what's told by the input it falls in, count again from the new client's first.
	p.held, p.view, p.pending, p.lastSeq, p.starved, p.deep = sim.Input{}, 0, nil, 0, 0, 0
	p.sprintEnds, p.spentAhead, p.hitSeqs, p.toldEnergy = nil, nil, map[string]uint64{}, -1
	p.dodged, p.dodgedSeq, p.blocked, p.blockedSeq = false, 0, false, 0
	snap := model.Snapshot{Tick: i.tick, MapID: i.m.ID, Channel: i.channel, Area: i.areaID(p.area), Pouches: i.ground.Views(id)}
	i.watchFrom(p, &snap)
	snap.FieldBoss, snap.FieldBossGone = i.bossState(p)
	if !sink.Send(snap) {
		i.hold(id, conn) // its stream went too: held again
		return model.View{}, ErrSinkClosed
	}
	f.away = 0
	i.welcome(p)
	if p.arenaBar != nil {
		p.private = append(p.private, model.ArenaBar{Tick: i.tick, Bar: p.arenaBar})
	}
	i.tellParty(p, i.m.ID, i.channel)
	s := i.stats(p)
	return model.View{
		ID: p.ID, Name: p.Name, Class: p.Class, Level: p.Level, Exp: p.Exp, Stats: s.Own,
		HP: p.HP, MaxHP: p.derived.MaxHP, Energy: p.Energy, MapID: i.m.ID, Sheet: s.Sheet,
		Quests: p.Quests.Clone(), Timers: i.timersOf(p, time.Now()), Waypoints: p.Waypoints.Clone(),
	}, nil
}

// dropAway takes off each fighter whose grace ran out at the tick's start, as a dropped connection was taken off before the grace: they've left, a bot stands in, and their next login lands where they came from.
func (i *Instance) dropAway() {
	mt := i.match
	if mt == nil {
		return
	}
	var out []kernel.CharacterID
	for _, side := range mt.order {
		for _, id := range side {
			if f := mt.fighters[id]; f.away != 0 && i.tick >= f.away {
				out = append(out, id)
			}
		}
	}
	for _, id := range out {
		mt.fighters[id].away = 0
		conn := i.players[id].Connection
		i.remove(id)
		i.mover.Dropped(i, id, conn)
	}
}
