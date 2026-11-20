package instance

import (
	"time"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
)

// stonesNear lists, for each interest cell, the waypoints whose finding circle may reach into it, so a tick reads only the player's cell's (large-regions.md section 8). A stone goes in every cell interest.of files a spot of the square round it in, a metre past reach so rounding never leaves one out; the find itself is exact.
func stonesNear(waypoints []gamedata.Waypoint, n *interest) [][]int {
	if len(waypoints) == 0 {
		return nil
	}
	out := make([][]int, n.nx*n.nz)
	r := gamedata.WaypointReach + 1.0
	for k, w := range waypoints {
		from, to := n.of(w.At.X-r, w.At.Z-r), n.of(w.At.X+r, w.At.Z+r)
		for row := from / n.nx; row <= to/n.nx; row++ {
			for col := from % n.nx; col <= to%n.nx; col++ {
				out[row*n.nx+col] = append(out[row*n.nx+col], k)
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
