package instance

import (
	"math"
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
)

// stonesNear lists, for each interest cell, the waypoints whose finding circle reaches into it, so a tick reads only the player's cell's (large-regions.md section 8). An edge cell holds what's past it, as interest.of does.
func stonesNear(waypoints []gamedata.Waypoint, n *interest) [][]int {
	if len(waypoints) == 0 {
		return nil
	}
	from := func(k int) float64 { // a cell's start along an axis, unbounded at the map's first
		if k == 0 {
			return math.Inf(-1)
		}
		return float64(k) * n.in.Cell
	}
	to := func(k, last int) float64 { // its end, unbounded at the last
		if k == last {
			return math.Inf(1)
		}
		return float64(k+1) * n.in.Cell
	}
	out := make([][]int, n.nx*n.nz)
	for c := range out {
		col, row := c%n.nx, c/n.nx
		x0, x1, z0, z1 := from(col), to(col, n.nx-1), from(row), to(row, n.nz-1)
		for k, w := range waypoints {
			if reaches(max(x0-w.At.X, 0, w.At.X-x1), max(z0-w.At.Z, 0, w.At.Z-z1)) {
				out[c] = append(out[c], k)
			}
		}
	}
	return out
}

// reaches reports whether a player dx and dz from a stone finds it: within WaypointReach, the edge counting.
func reaches(dx, dz float64) bool {
	return float64(dx*dx)+float64(dz*dz) <= gamedata.WaypointReach*gamedata.WaypointReach
}

// trackWaypoints finds, for each living player, the stones their cell lists within reach that they hadn't found, telling them; a find is theirs for good, kept by the checkpoint.
func (i *Instance) trackWaypoints(now time.Time) {
	if i.stones == nil {
		return
	}
	for _, id := range i.order {
		p := i.players[id]
		if p.dead || p.bot != nil {
			continue
		}
		for _, k := range i.stones[i.interest.of(p.state.X, p.state.Z)] {
			w := i.m.Waypoints[k]
			if _, found := p.Waypoints[w.ID]; found || !reaches(p.state.X-w.At.X, p.state.Z-w.At.Z) {
				continue
			}
			p.Waypoints[w.ID] = now
			p.private = append(p.private, model.WaypointFound{Tick: i.tick, Waypoint: w.ID})
		}
	}
}
