// Package steer walks a bot as a player's keys would: the keys toward a way, the yaw to a point, a walker along nav paths, and the stall it watches for. cmd/bot and a match's bots both walk with it.
package steer

import (
	"math"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

const (
	waypointReach = 0.3         // metres from a path's waypoint the walker turns to the next
	repathEvery   = time.Second // a path is found again this often, or sooner when its end moves
	repathMoved   = 1.0         // metres its end may move before the path is found again
	stallMoved    = 0.05        // metres a body pressing a way must move in stallFor decisions not to have stalled
	stallFor      = 4           // decisions
	asideFor      = 2           // decisions a stalled walker steps aside
)

// KeysToward are the keys whose way is nearest (dx, dz), x east and z north: one key, or two for a diagonal, each within 22.5° of the way.
func KeysToward(dx, dz float64) sim.Input {
	l := math.Hypot(dx, dz)
	if l == 0 {
		return sim.Input{}
	}
	s := l * math.Sin(math.Pi/8)
	return sim.Input{Right: dx > s, Left: dx < -s, Up: dz > s, Down: dz < -s}
}

// YawTo is the aim from one point to another: radians from north toward east.
func YawTo(from, to nav.Point) float32 {
	return float32(math.Atan2(to.X-from.X, to.Z-from.Z))
}

// wayOf is the way an input's keys press, x east and z north, each -1, 0 or 1.
func wayOf(in sim.Input) [2]int {
	key := func(plus, minus bool) int {
		switch {
		case plus && !minus:
			return 1
		case minus && !plus:
			return -1
		}
		return 0
	}
	return [2]int{key(in.Right, in.Left), key(in.Up, in.Down)}
}

// Walker turns nav paths into keys, finding the path again when its end moves, a second on, or after a stall. Its zero value is ready.
type Walker struct {
	finder nav.Finder
	path   []nav.Point
	mapID  string
	to     nav.Point
	found  time.Duration
	none   bool // the last search found no way
	stalls int  // since it last reached a waypoint
	aside  int  // decisions left stepping aside
}

// Keys walks a body of radius r from from toward to on m, round what's in the way, jumping then stepping aside at a stall.
func (w *Walker) Keys(m *gamedata.Map, r float64, from, to nav.Point, now time.Duration, stalled bool) sim.Input {
	if w.mapID != m.ID || nav.Distance(w.to, to) > repathMoved || now-w.found > repathEvery || (!w.none && (stalled || len(w.path) == 0)) {
		leash := math.Hypot(m.Geometry.SizeX, m.Geometry.SizeZ)
		w.path = w.finder.Path(m.Grid(), from, to, r+gamedata.BotPathMargin, from, leash)
		if len(w.path) == 0 {
			w.path = w.finder.Path(m.Grid(), from, to, r, from, leash)
		}
		if n := len(w.path); n > 0 {
			w.path[n-1] = to
		}
		w.mapID, w.to, w.found, w.none = m.ID, to, now, len(w.path) == 0
	}
	for len(w.path) > 1 && nav.Distance(from, w.path[0]) < waypointReach {
		w.path, w.stalls = w.path[1:], 0
	}
	next := to
	if len(w.path) > 0 {
		next = w.path[0]
	}
	dx, dz := next.X-from.X, next.Z-from.Z
	if stalled {
		w.stalls++
		if w.stalls%2 == 1 {
			in := KeysToward(dx, dz)
			in.Jump = true
			return in
		}
		w.aside = asideFor
	}
	if w.aside > 0 {
		w.aside--
		side := float64(w.stalls%4 - 1) // 1 at the second stall, -1 at the fourth
		return KeysToward(-dz*side, dx*side)
	}
	return KeysToward(dx, dz)
}

// Lost reports whether the last search found no way to the walker's end.
func (w *Walker) Lost() bool { return w.none }

// Stall finds a body pressing one way on the ground that barely moves, counting only frames that could move it. Its zero value is ready.
type Stall struct {
	at    nav.Point
	way   [2]int
	count int
}

// Stalled takes a decision's position and the input held into it, and reports whether the body has just stalled.
func (s *Stall) Stalled(at nav.Point, onGround bool, held sim.Input) bool {
	way := wayOf(held)
	if !onGround || way == ([2]int{}) || way != s.way || nav.Distance(at, s.at) >= stallMoved {
		s.count, s.at, s.way = 0, at, way
		return false
	}
	s.count++
	if s.count < stallFor {
		return false
	}
	s.count = 0
	return true
}
