package e2etest

import (
	"time"

	"finga/pkg/wsconn"
)

// Game is a wait's budget of the world's time, spent by the frames its player reads: a starved runner pauses the world and the test together, and the budget waits with them.
type Game struct {
	p           *Player
	until, seen uint64    // the player's elapsed count the budget runs out at, and as last looked
	heard       time.Time // when that count last moved
}

// Game starts a budget of d of the world's ticks.
func (p *Player) Game(d time.Duration) *Game {
	return &Game{p: p, until: p.elapsed + p.ticksIn(d), seen: p.elapsed, heard: time.Now()}
}

// Left is whether some of the budget is left and a frame has come within Stalled.
func (g *Game) Left() bool {
	if g.p.elapsed != g.seen {
		g.seen, g.heard = g.p.elapsed, time.Now()
	}
	return g.p.elapsed < g.until && time.Since(g.heard) < Stalled
}

// Remaining is what's left of the budget as the world's time, for a wait inside the one it bounds.
func (g *Game) Remaining() time.Duration {
	if g.p.elapsed >= g.until || g.p.TickHz == 0 {
		return 0
	}
	return time.Duration(g.until-g.p.elapsed) * time.Second / time.Duration(g.p.TickHz)
}

// Await reads until pred holds for a message; it gives up once d of the world's ticks pass, when no frame comes for Stalled, or when the connection closes.
func (p *Player) Await(d time.Duration, pred func(wsconn.Message) bool) bool {
	for game := p.Game(d); game.Left(); {
		m, ok := p.Read(time.Second)
		if ok && pred(m) {
			return true
		}
		if !ok && p.Closed(0) {
			return false
		}
	}
	return false
}

// Reach reads until a frame of tick or later, on the player's map, comes.
func (p *Player) Reach(tick uint64) {
	p.t.Helper()
	if !p.Await(Stalled, func(m wsconn.Message) bool { return m.Type == "Delta" && m.Tick >= tick }) {
		p.t.Fatalf("%s: no frame reached tick %d; the last was %d", p.ID, tick, p.tick)
	}
}

// step counts a frame of tick read: the ticks since the last on the same map, or one for a new map's first.
func (p *Player) step(tick uint64) {
	if p.moved || p.tick == 0 || tick < p.tick {
		p.elapsed++
	} else {
		p.elapsed += tick - p.tick
	}
	p.tick, p.moved = tick, false
}

// ticksIn is d in the world's ticks.
func (p *Player) ticksIn(d time.Duration) uint64 {
	return uint64(d.Seconds() * float64(p.TickHz))
}
