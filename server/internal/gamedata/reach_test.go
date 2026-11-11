package gamedata_test

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"finga/internal/gamedata"
	"finga/internal/sim"
)

// A resting place is where a player stops between moves: standing, or holding still on a rope.
type place struct {
	x, y     int // feet, in steps of reachGrid
	ground   int
	climbing bool
}

const (
	reachGrid = 8   // px between the places a player is tried from
	maxAir    = 600 // sub-steps a fall may take before the route counts as lost (10 s)
)

// mapReach is every place a player can reach on a map from the places they arrive at, and the moves between them.
type mapReach struct {
	m      *gamedata.Map
	p      sim.Params
	states map[place]sim.State
	next   map[place]map[place]bool
	starts []place // where players arrive
}

func keyOf(s sim.State) place {
	return place{x: int(math.Round(s.X / reachGrid)), y: int(math.Round(s.Y)), ground: s.Ground, climbing: s.Climbing}
}

// rest plays held from s until the player stands, grabs a rope or holds still on one, at the earliest after settle sub-steps; false if they never do.
func (r *mapReach) rest(s sim.State, settle int, held func(step int) sim.Input) (sim.State, bool) {
	for step := range maxAir {
		in := held(step)
		was := s.Climbing
		s = r.step(s, in)
		if step+1 >= settle && (s.OnGround || s.Climbing && (!was || !in.Up && !in.Down)) {
			s.VX, s.VY = 0, 0
			return s, true
		}
	}
	return s, false
}

func (r *mapReach) step(s sim.State, in sim.Input) sim.State {
	return sim.Step(s, in, r.m.Geometry, r.p)
}

// moves are what a player can do from a resting place, each played until they rest again.
func (r *mapReach) moves(s sim.State) []sim.State {
	var out []sim.State
	add := func(s sim.State, ok bool) {
		if ok {
			out = append(out, s)
		}
	}
	walkTicks := int(math.Ceil(reachGrid / (r.p.WalkSpeed * r.p.Dt)))
	climbTicks := int(math.Ceil(2 * reachGrid / (r.p.ClimbSpeed * r.p.Dt)))
	sides := []sim.Input{{Left: true}, {Right: true}}
	if !s.Climbing {
		// A step along the floor, or off its edge, steering on or letting go once in the air.
		for _, side := range sides {
			for _, steer := range []bool{true, false} {
				add(r.rest(s, walkTicks, func(step int) sim.Input {
					if step < walkTicks || steer {
						return side
					}
					return sim.Input{}
				}))
			}
		}
	}
	// Jumps straight up or to a side, letting go of the side after a while, reaching for a rope or not.
	for _, side := range append([]sim.Input{{}}, sides...) {
		for _, hold := range []int{0, 8, 16, 24, maxAir} {
			if hold > 0 && side == (sim.Input{}) {
				continue
			}
			for _, reach := range []bool{false, true} {
				add(r.rest(s, 1, func(step int) sim.Input {
					in := sim.Input{Jump: step == 0, Up: reach && step > 0}
					if step < hold {
						in.Left, in.Right = side.Left, side.Right
					}
					return in
				}))
			}
		}
	}
	if s.OnGround && s.Ground != sim.NoPlatform {
		// Through the platform, onto what is below or onto a rope passing it.
		for _, reach := range []bool{false, true} {
			add(r.rest(s, 1, func(step int) sim.Input { return sim.Input{Down: step == 0 || reach, Jump: step == 0} }))
		}
	}
	// Up or down a rope a stretch at a time, or off its end.
	for _, way := range []sim.Input{{Up: true}, {Down: true}} {
		if c := r.step(s, way); c.Climbing {
			add(r.rest(c, climbTicks, func(step int) sim.Input {
				if step < climbTicks-1 {
					return way
				}
				return sim.Input{}
			}))
		}
	}
	return out
}

// explore finds every place reachable from the map's player spawn and its portals' arrivals.
func explore(t *testing.T, m *gamedata.Map, p sim.Params) *mapReach {
	t.Helper()
	r := &mapReach{m: m, p: p, states: map[place]sim.State{}, next: map[place]map[place]bool{}}
	var queue []place
	visit := func(s sim.State) place {
		k := keyOf(s)
		if _, ok := r.states[k]; !ok {
			r.states[k] = s
			r.next[k] = map[place]bool{}
			queue = append(queue, k)
		}
		return k
	}
	starts := []gamedata.Point{m.PlayerSpawn}
	for _, pt := range m.Portals {
		starts = append(starts, pt.Arrival())
	}
	for _, at := range starts {
		s, ok := r.rest(sim.State{X: at.X, Y: at.Y - 1, Facing: 1, Ground: sim.NoPlatform, DropPlatform: sim.NoPlatform}, 1, func(int) sim.Input { return sim.Input{} })
		if !ok {
			t.Fatalf("%s: a player arriving at %+v never lands", m.ID, at)
		}
		r.starts = append(r.starts, visit(s))
	}
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		for _, s := range r.moves(r.states[k]) {
			r.next[k][visit(s)] = true
		}
	}
	return r
}

// standsIn reports whether a resting player's feet are inside a.
func standsIn(s sim.State, a sim.Rect) bool {
	return s.X >= a.X && s.X <= a.X+a.W && s.Y >= a.Y && s.Y <= a.Y+a.H
}

// speeds are the factors a map is played at: a town at the base speed, a hunting map from it up to the fastest move speed.
func speeds(d *gamedata.Data, m *gamedata.Map) []float64 {
	if m.Kind == gamedata.Town {
		return []float64{1}
	}
	return []float64{1, sim.SpeedFactor(d.StatRules().Curves.MoveSpeed.Limit)}
}

// On every map, at each speed it is played at, wherever a player gets to they can still walk to each portal, and from where they arrive they can reach each spawn area.
// Each platform and block top is a step up from below, so none is reached only by a detour over it. A faster character jumps a little lower, and the test's jump is stepMargin lower still.
func TestEveryPortalAndSpawnCanBeReached(t *testing.T) {
	d, err := gamedata.Load("../../data")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range d.Maps() {
		for _, f := range speeds(d, m) {
			t.Run(fmt.Sprintf("%s at %g×", m.ID, f), func(t *testing.T) {
				t.Parallel()
				p := shortOf(d.Movement().Faster(f), stepMargin)
				r := explore(t, m, p)
				for _, pt := range m.Portals {
					if cut := r.cutOff(pt.Area); len(cut) > 0 {
						t.Errorf("portal %s at %+v: no way to it from %d places, e.g. feet at %s", pt.Name, pt.Area, len(cut), cut[0])
					}
				}
				for k, sp := range m.Spawns {
					if !r.reaches(func(s sim.State) bool { return standsIn(s, sp.Area) }) {
						t.Errorf("spawn %d (%s) at %+v: no player can reach it", k, sp.Mob, sp.Area)
					}
				}
				for i, pl := range m.Geometry.Platforms {
					on := func(s sim.State) bool { return s.OnGround && s.Ground == i }
					if !r.reaches(on) {
						t.Errorf("platform %d at %+v: no player can stand on it", i, pl)
					} else if !r.steppedOnto(on) {
						t.Errorf("platform %d at %+v: reached only from above or beside it", i, pl)
					}
				}
				for i, so := range m.Geometry.Solids {
					on := func(s sim.State) bool {
						return s.OnGround && s.Ground == sim.NoPlatform && s.Y == so.Y && s.X+p.Width/2 > so.X && s.X-p.Width/2 < so.X+so.W
					}
					if r.reaches(on) && !r.arrivesOn(on) && !r.steppedOnto(on) {
						t.Errorf("solid %d at %+v: its top is reached only from above or beside it", i, so)
					}
				}
				t.Logf("%d resting places", len(r.states))
			})
		}
	}
}

// stepMargin is how much lower than the real one the test's jump peaks, so a step it makes, a player makes with room to spare.
const stepMargin = 4

// shortOf is p with a jump that peaks about margin px lower.
func shortOf(p sim.Params, margin float64) sim.Params {
	peak := jumpPeak(p)
	p.JumpVelocity *= math.Sqrt((peak - margin) / peak)
	return p
}

// jumpPeak is how high a standing jump rises at p, stepped as the game steps it.
func jumpPeak(p sim.Params) float64 {
	g := sim.Geometry{Solids: []sim.Rect{{X: -1000, Y: 0, W: 2000, H: 32}}}
	s := sim.State{Facing: 1, OnGround: true, Ground: sim.NoPlatform, DropPlatform: sim.NoPlatform}
	peak := 0.0
	for step := range maxAir {
		s = sim.Step(s, sim.Input{Jump: step == 0}, g, p)
		peak = max(peak, -s.Y)
	}
	return peak
}

// steppedOnto reports whether a move from lower down ends where goal holds: a jump or a climb up onto it.
func (r *mapReach) steppedOnto(goal func(sim.State) bool) bool {
	for from, tos := range r.next {
		for to := range tos {
			if goal(r.states[to]) && r.states[from].Y > r.states[to].Y {
				return true
			}
		}
	}
	return false
}

func (r *mapReach) arrivesOn(goal func(sim.State) bool) bool {
	return slices.ContainsFunc(r.starts, func(k place) bool { return goal(r.states[k]) })
}

func (r *mapReach) reaches(goal func(sim.State) bool) bool {
	for _, s := range r.states {
		if goal(s) {
			return true
		}
	}
	return false
}

// cutOff are the reachable places from which no chain of moves leads to standing in a.
func (r *mapReach) cutOff(a sim.Rect) []string {
	prev := map[place][]place{}
	for from, tos := range r.next {
		for to := range tos {
			prev[to] = append(prev[to], from)
		}
	}
	leads := map[place]bool{}
	var queue []place
	for k, s := range r.states {
		if s.OnGround && standsIn(s, a) {
			leads[k] = true
			queue = append(queue, k)
		}
	}
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		for _, p := range prev[k] {
			if !leads[p] {
				leads[p] = true
				queue = append(queue, p)
			}
		}
	}
	var out []string
	for k, s := range r.states {
		if !leads[k] {
			out = append(out, fmt.Sprintf("(%.0f, %.0f)", s.X, s.Y))
		}
	}
	slices.Sort(out)
	return out
}
