package instance

import (
	"math"

	model "finga/internal/model/world"
	"finga/internal/shared/gamedata"
)

// areaID is the id of the region's area at index a, empty for none.
func (i *Instance) areaID(a int) string {
	if a < 0 {
		return ""
	}
	return i.m.Areas[a].ID
}

// trackAreas tells each player who stepped into another of the region's areas which one.
func (i *Instance) trackAreas() {
	if len(i.m.Areas) == 0 {
		return
	}
	for _, id := range i.order {
		p := i.players[id]
		if a := i.m.AreaAt(p.state.X, p.state.Z); a != p.area {
			p.area = a
			p.private = append(p.private, model.AreaEntered{Tick: i.tick, Area: i.areaID(a)})
		}
	}
}

// getUp is where one who died at (x, z) gets up on a map that revives its own dead: its nearest waypoint in a straight line, found or not, on a region, whose towns each hold one; the map's spawn on a town.
func (i *Instance) getUp(x, z float64) gamedata.Point {
	at, best := i.m.PlayerSpawn, math.Inf(1)
	for _, w := range i.m.Waypoints {
		if d := math.Hypot(w.At.X-x, w.At.Z-z); d < best {
			at, best = w.At, d
		}
	}
	return at
}
