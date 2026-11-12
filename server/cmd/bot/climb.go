package main

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"time"

	clientv1 "finga/api/gen/client/v1"
	"finga/internal/gamedata"
	"finga/internal/sim"
	"finga/pkg/tiled"
)

// climbRoutes are the ways bots take to spots off a map's ground, by map: the ropes they may climb, by name, then the platform they lead to.
var climbRoutes = map[string][]string{
	"cave": {"floor_rope", "shelf_rope", "pillar_rope", "pillar_top"}, // the bats, for the Thief's trial
}

// A bot tries a climb for climbLimit before it gives up on the spot and hunts the ground, then tries again after climbRetry.
const (
	climbLimit = 3 * time.Minute
	climbRetry = 10 * time.Minute
)

// climb is a way up a map's ropes to one of its platforms, read from the map's named areas, so a renamed or moved one fails at start.
type climb struct {
	ropes  []sim.Rect // each from the level of its foot to the level of its top
	to     sim.Rect   // the platform it reaches
	solids []sim.Rect
	jump   float64 // the highest a jump clears
}

func loadClimbs(dataDir string, data *gamedata.Data) (map[string]*climb, error) {
	mv := data.Movement()
	jump := mv.JumpVelocity * mv.JumpVelocity / (2 * mv.Gravity)
	out := map[string]*climb{}
	for mapID, names := range climbRoutes {
		m, ok := data.Map(mapID)
		if !ok {
			return nil, fmt.Errorf("climb: no map %q", mapID)
		}
		tm, err := tiled.Load(filepath.Join(dataDir, "maps", mapID+".tmj"))
		if err != nil {
			return nil, err
		}
		groups := tm.ObjectGroups()
		find := func(layer, name string) (sim.Rect, error) {
			for _, o := range groups[layer].Objects {
				if o.Name == name {
					return sim.Rect{X: o.X, Y: o.Y, W: o.Width, H: o.Height}, nil
				}
			}
			return sim.Rect{}, fmt.Errorf("climb: %s has no %q in its %s layer", mapID, name, layer)
		}
		c := &climb{solids: m.Geometry.Solids, jump: jump}
		for _, name := range names[:len(names)-1] {
			r, err := find("ropes", name)
			if err != nil {
				return nil, err
			}
			c.ropes = append(c.ropes, r)
		}
		if c.to, err = find("platforms", names[len(names)-1]); err != nil {
			return nil, err
		}
		if !slices.ContainsFunc(c.ropes, func(r sim.Rect) bool { return level(r.Y, c.to.Y) }) {
			return nil, fmt.Errorf("climb: no rope of the %s climb tops out on its platform", mapID)
		}
		out[mapID] = c
	}
	return out, nil
}

// level reports whether two heights are the same level.
func level(a, b float64) bool { return math.Abs(a-b) < 1 }

// on reports whether feet at (x, y) stand on the climb's platform.
func (c *climb) on(x, y float64) bool {
	return level(y, c.to.Y) && x >= c.to.X && x <= c.to.X+c.to.W
}

// step is the input that takes feet at (x, y) on toward the platform: up a rope it hangs on, on its way while in the air, catching a rope it passes, to and up the nearest rope whose foot is its level with nothing too tall between, else toward the platform, which a walk off a higher one reaches.
func (c *climb) step(x, y float64, climbing, onGround bool, held *clientv1.Input) *clientv1.Input {
	if climbing {
		return &clientv1.Input{Up: true}
	}
	if !onGround {
		// In the air it keeps its way: turning for the platform could drive it back into a wall it then jumps against.
		return &clientv1.Input{Left: held.GetLeft(), Right: held.GetRight(), Up: c.passing(x, y)}
	}
	best, far := 0.0, math.Inf(1)
	for _, r := range c.ropes {
		mid := r.X + r.W/2
		if d := math.Abs(mid - x); level(y, r.Y+r.H) && d < far && !c.blocked(y, x, mid) {
			best, far = mid, d
		}
	}
	if math.IsInf(far, 1) {
		return toward(x, c.to.X+c.to.W/2)
	}
	if far < 12 {
		return &clientv1.Input{Up: true}
	}
	return toward(x, best)
}

// passing reports whether feet at (x, y) in the air are over a rope that leads up, which Up catches, as a jump toward one would carry it past.
func (c *climb) passing(x, y float64) bool {
	return slices.ContainsFunc(c.ropes, func(r sim.Rect) bool {
		return y > r.Y && y <= r.Y+r.H && math.Abs(r.X+r.W/2-x) < 12
	})
}

// blocked reports whether a solid too tall to jump stands between x1 and x2 on level y.
func (c *climb) blocked(y, x1, x2 float64) bool {
	lo, hi := min(x1, x2), max(x1, x2)
	for _, s := range c.solids {
		if s.X < hi && s.X+s.W > lo && s.Y < y && s.Y+s.H >= y && y-s.Y > c.jump {
			return true
		}
	}
	return false
}

func toward(x, target float64) *clientv1.Input {
	return &clientv1.Input{Right: target > x, Left: target < x}
}
