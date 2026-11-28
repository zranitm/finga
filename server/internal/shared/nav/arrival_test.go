package nav_test

import (
	"fmt"
	"testing"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// arrivalMargin is how far past a mob's sight every spawn and boss circle keeps from where players come in or talk, so a mob at its circle's edge doesn't see them.
const arrivalMargin = 2.0

// place is somewhere players come in or talk.
type place struct {
	what string
	at   nav.Point
}

// home is where a mob lives: a camp or a boss circle.
type home struct {
	what, mob string
	at        nav.Point
	r         float64
}

// ground is a shipped region's land or place below, as the design tests read it: where players come in, and where mobs live.
type ground struct {
	id       string
	geo      sim.Geometry
	grid     func() *nav.Grid
	arrivals []place // the player spawn, portals, NPCs, and a region's waypoints' arrive points
	starts   []nav.Point
	homes    []home
}

// huntingGrounds are every shipped region's hunting areas, which the maps' three design tests check.
func huntingGrounds(t *testing.T, data *gamedata.Data) []ground {
	t.Helper()
	var out []ground
	for id := range shippedRegions(t) {
		if id == "test_region" {
			continue // a generator fixture, with gaps known
		}
		m, ok := data.Map(id)
		if !ok {
			t.Fatalf("no region %s in the data", id)
		}
		g := ground{id: id, geo: m.Geometry, grid: m.Grid, starts: []nav.Point{{X: m.PlayerSpawn.X, Z: m.PlayerSpawn.Z}}}
		g.arrivals = []place{{"the player spawn", g.starts[0]}}
		for _, pt := range m.Portals {
			at := nav.Point{X: pt.At.X, Z: pt.At.Z}
			g.arrivals = append(g.arrivals, place{"gate " + pt.Name, at})
			g.starts = append(g.starts, at)
		}
		for _, n := range m.NPCs {
			g.arrivals = append(g.arrivals, place{"NPC " + n.NPC, nav.Point{X: n.At.X, Z: n.At.Z}})
		}
		for _, w := range m.Waypoints {
			at := nav.Point{X: w.Arrive.X, Z: w.Arrive.Z}
			g.arrivals = append(g.arrivals, place{"waypoint " + w.ID + "'s arrive point", at})
			g.starts = append(g.starts, at)
		}
		for _, s := range m.Spawns {
			g.homes = append(g.homes, home{"spawn " + s.Mob + " at " + pointName(s.At), s.Mob, nav.Point{X: s.At.X, Z: s.At.Z}, s.Radius})
		}
		for _, a := range m.Areas {
			if a.Kind == gamedata.Hunting && a.Boss != nil {
				g.homes = append(g.homes, home{"area " + a.ID + "'s boss", a.Boss.Mob, nav.Point{X: a.Boss.At.X, Z: a.Boss.At.Z}, a.Boss.Radius})
			}
		}
		out = append(out, g)
	}
	return out
}

func pointName(p gamedata.Point) string { return fmt.Sprintf("(%g, %g)", p.X, p.Z) }

// On every hunting map and every shipped region's hunting areas, no mob sees a player arrive or stand at an NPC: each spawn and boss keep their circles a mob's sight plus arrivalMargin from the player spawn, every portal, every NPC and every waypoint's arrive point, so a fight is one a player walks into.
func TestNoSpawnSeesAnArrivalOrAnNPC(t *testing.T) {
	data := gamedatatest.Full(t)
	clear := data.MobRules().Sight + arrivalMargin
	for _, g := range huntingGrounds(t, data) {
		t.Run(g.id, func(t *testing.T) {
			for _, a := range g.arrivals {
				for _, h := range g.homes {
					if gap := nav.Distance(a.at, h.at) - h.r; gap < clear {
						t.Errorf("%s's circle comes within %.2f m of %s; it keeps %.2f m, a mob's sight and %.0f m more", h.what, gap, a.what, clear, arrivalMargin)
					}
				}
			}
		})
	}
}
