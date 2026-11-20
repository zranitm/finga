package e2etest

import (
	"fmt"
	"math"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
	"finga/internal/shared/steer"
	"finga/pkg/wsconn"
)

// Walk is a looter's way toward a point, as the bots walk: round solids on the map's paths, jumping and then stepping aside when the body stalls, as a mob standing in the way holds it. Its zero value is ready.
type Walk struct {
	walker  steer.Walker
	stall   steer.Stall
	held    sim.Input
	decided uint64 // the tick of the frame the keys were last chosen on
	started bool
}

// Step sends the keys toward to, at most once a frame, so a stall is counted in the world's frames whatever the caller's loop.
func (l *Looter) Step(w *Walk, to gamedata.Point) error {
	if w.started && w.decided == l.tick {
		return nil
	}
	data, err := l.stack.gameData()
	if err != nil {
		return err
	}
	m, ok := data.Map(l.MapID)
	if !ok {
		return fmt.Errorf("no map %q to walk on", l.MapID)
	}
	at := nav.Point{X: l.X, Z: l.Z}
	stalled := w.stall.Stalled(at, l.onGround, w.held)
	in := w.walker.Keys(m, data.Movement().Radius, at, nav.Point{X: to.X, Z: to.Z}, l.worldTime(), stalled)
	w.held, w.decided, w.started = in, l.tick, true
	return l.Input(&clientv1.Input{Left: in.Left, Right: in.Right, Up: in.Up, Down: in.Down, Jump: in.Jump})
}

// WalkTo walks to within 0.375 m of to and stands there, still on the ground, by Step; it gives up once d of the world's ticks pass, or when no frame comes for Stalled.
func (l *Looter) WalkTo(to gamedata.Point, d time.Duration) bool {
	var w Walk
	var stop uint32 // the first stop's seq, once near
	for start := l.tick; l.tick-start < l.ticksIn(d); {
		switch near := math.Hypot(to.X-l.X, to.Z-l.Z); {
		case stop != 0 && l.ack >= stop && near < 0.75 && l.Standing():
			return true
		case near < 0.375:
			if err := l.Input(&clientv1.Input{}); err != nil {
				return false
			}
			if stop == 0 {
				stop, w = l.Seq, Walk{}
			}
		default: // still walking, or slid off while stopping
			stop = 0
			if l.Step(&w, to) != nil {
				return false
			}
		}
		if !l.nextFrame() {
			return false
		}
	}
	_ = l.Input(&clientv1.Input{})
	return false
}

// Stalled is how long a wait for the world's ticks goes without a frame before the stack has stopped: frames come every tick, and a pause of the test's own process, as a starved runner makes, stops the world and the reader together, so it's waited out.
const Stalled = time.Minute

// nextFrame reads until a frame newer than the last one has come, for up to Stalled, then what has come behind it: a walk decides on the newest frame, however far behind its reader runs.
func (l *Looter) nextFrame() bool {
	was := l.tick
	if !l.Pump(Stalled, func(wsconn.Message) bool { return l.tick != was }) {
		return false
	}
	for m, ok := l.Read(0); ok; m, ok = l.Read(0) {
		l.Observe(m)
	}
	return true
}

// ticksIn is d in the world's ticks.
func (l *Looter) ticksIn(d time.Duration) uint64 {
	return uint64(d.Seconds() * float64(l.TickHz))
}

// worldTime is the newest frame's tick as time on the map's clock.
func (l *Looter) worldTime() time.Duration {
	if l.TickHz == 0 {
		return 0
	}
	return time.Duration(l.tick) * time.Second / time.Duration(l.TickHz)
}
