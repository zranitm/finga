package e2etest

import (
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/pkg/wsconn"
)

// Game is a wait's budget of the world's time, spent by the frames its player reads: a starved runner pauses the world and the test together, and the budget waits with them.
type Game struct {
	p           *Player
	until, seen uint64    // the player's elapsed count the budget runs out at, and as last looked
	heard       time.Time // when that count last moved
	wall        time.Time // the end on the wall clock, before the Welcome tells the tick rate: no frames come to count then
}

// Game starts a budget of d of the world's ticks from the newest frame come, read or not: frames queued while the test was busy elsewhere are past, not the wait's, so they're held for the next reads and counted as spent before the budget.
func (p *Player) Game(d time.Duration) *Game {
	for m, ok := p.receive(0); ok; m, ok = p.receive(0) {
		p.held = append(p.held, m)
	}
	g := &Game{p: p, until: p.elapsed + p.stepsIn(p.held) + p.ticksIn(d), seen: p.elapsed, heard: time.Now()}
	if p.TickHz == 0 {
		g.wall = time.Now().Add(d)
	}
	return g
}

// Left is whether some of the budget is left and a frame has come within Stalled.
func (g *Game) Left() bool {
	if !g.wall.IsZero() {
		return time.Now().Before(g.wall)
	}
	if g.p.elapsed != g.seen {
		g.seen, g.heard = g.p.elapsed, time.Now()
	}
	return g.p.elapsed < g.until && time.Since(g.heard) < Stalled
}

// Remaining is what's left of the budget as the world's time, for a wait inside the one it bounds.
func (g *Game) Remaining() time.Duration {
	if !g.wall.IsZero() {
		return max(0, time.Until(g.wall))
	}
	if g.p.elapsed >= g.until || g.p.TickHz == 0 {
		return 0
	}
	return time.Duration(g.until-g.p.elapsed) * time.Second / time.Duration(g.p.TickHz)
}

// Next reads the next message while the budget lasts; false once it's spent, no frame comes for Stalled, or the connection closes.
func (g *Game) Next() (wsconn.Message, bool) {
	for g.Left() {
		if m, ok := g.p.Read(time.Second); ok {
			return m, true
		}
		if g.p.Closed(0) {
			return wsconn.Message{}, false
		}
	}
	return wsconn.Message{}, false
}

// Await reads until pred, when set, holds for a message; it gives up once d of the world's ticks pass, when no frame comes for Stalled, or when the connection closes.
func (p *Player) Await(d time.Duration, pred func(wsconn.Message) bool) bool {
	for game := p.Game(d); ; {
		m, ok := game.Next()
		if !ok {
			return false
		}
		if pred != nil && pred(m) {
			return true
		}
	}
}

// Tick is the newest frame's tick, in the player's map's ticks.
func (p *Player) Tick() uint64 { return p.tick }

// Reach reads until a frame of tick or later, on the player's map, comes.
func (p *Player) Reach(tick uint64) {
	p.t.Helper()
	if !p.Await(Stalled, func(m wsconn.Message) bool { return m.Type == "Delta" && m.Tick >= tick }) {
		p.t.Fatalf("%s: no frame reached tick %d; the last was %d", p.ID, tick, p.tick)
	}
}

// step counts a frame of tick read: the ticks since the last on the same map, or one for a new map's first.
func (p *Player) step(tick uint64) {
	p.elapsed += stepOf(p.tick, p.moved, tick)
	p.tick, p.moved = tick, false
}

// stepsIn is what reading msgs would add to the count.
func (p *Player) stepsIn(msgs []wsconn.Message) uint64 {
	tick, moved, n := p.tick, p.moved, uint64(0)
	for _, m := range msgs {
		switch m.Data.(type) {
		case *clientv1.MapChange:
			moved = true
		case *clientv1.Delta, *clientv1.Snapshot:
			n += stepOf(tick, moved, m.Tick)
			tick, moved = m.Tick, false
		}
	}
	return n
}

// stepOf is the world's ticks a frame of tick counts after one of last: those between them on one map, or one for a new map's first, a restarted world's, or the first known.
func stepOf(last uint64, moved bool, tick uint64) uint64 {
	if moved || last == 0 || tick < last {
		return 1
	}
	return tick - last
}

// ticksIn is d in the world's ticks.
func (p *Player) ticksIn(d time.Duration) uint64 {
	return uint64(d.Seconds() * float64(p.TickHz))
}
