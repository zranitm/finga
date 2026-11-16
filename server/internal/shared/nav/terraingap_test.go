package nav_test

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"gopkg.in/yaml.v3"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// gapReach is how far from a solid the gap check looks for terrain, past the widest mob.
const gapReach = 6.0

// half is half the grid's spacing: a steep sample stands for the terrain cell around it.
const half = 0.25

// solid is a solid for the gap check: its name, the ground at its foot, and its footprint's nearest point to p.
type solid struct {
	what    string
	base    float64
	nearest func(p nav.Point) nav.Point
}

// gap is a narrowest spot between a solid and terrain too steep to walk: how wide, its middle, and the widest mob whose leash reaches it.
type gap struct {
	what   string
	width  float64
	middle nav.Point
	mob    string
	across float64
}

func (g gap) String() string {
	return fmt.Sprintf("%s leaves a gap of %.2f m to the terrain at (%.2f, %.2f), wide enough for a player and too narrow for the %s, %.2f m across", g.what, g.width, g.middle.X, g.middle.Z, g.mob, g.across)
}

// solids are m's solids that are walls to someone walking beside them.
func solids(m *mapfile.Map, p sim.Params) []solid {
	geo := m.Geometry
	var out []solid
	for _, c := range geo.Cylinders {
		base := geo.Terrain.Height(c.X, c.Z)
		if c.Top <= base+p.Step {
			continue
		}
		at := nav.Point{X: c.X, Z: c.Z}
		out = append(out, solid{fmt.Sprintf("the cylinder at (%g, %g)", c.X, c.Z), base, func(q nav.Point) nav.Point {
			d := nav.Distance(q, at)
			if d <= c.Radius {
				return q
			}
			return nav.Point{X: c.X + (q.X-c.X)*c.Radius/d, Z: c.Z + (q.Z-c.Z)*c.Radius/d}
		}})
	}
	for _, b := range geo.Boxes {
		base := geo.Terrain.Height(b.X, b.Z)
		if b.Top <= base+p.Step {
			continue
		}
		out = append(out, solid{fmt.Sprintf("the box at (%g, %g)", b.X, b.Z), base, func(q nav.Point) nav.Point {
			dx, dz := q.X-b.X, q.Z-b.Z
			u := min(max(dx*b.UX+dz*b.UZ, -b.HalfW), b.HalfW)
			v := min(max(dz*b.UX-dx*b.UZ, -b.HalfD), b.HalfD)
			return nav.Point{X: b.X + u*b.UX - v*b.UZ, Z: b.Z + u*b.UZ + v*b.UX}
		}})
	}
	return out
}

// terrainGaps is each narrowest gap on m between a solid and terrain too steep to walk that rises over the solid's foot, where a player fits and the widest mob whose leash reaches its middle doesn't. A gap is narrowest where the solid's distance to the terrain is least along it, so a solid hugging a bank has one gap, the hug.
func terrainGaps(t *testing.T, data *gamedata.Data, m *mapfile.Map, p sim.Params) []gap {
	t.Helper()
	geo := m.Geometry
	type sample struct {
		at  nav.Point
		top float64
	}
	var steep []sample
	for z := half; z < m.Size[1]; z += 2 * half {
		for x := half; x < m.Size[0]; x += 2 * half {
			if geo.Terrain.Steep(x, z, false) || geo.Terrain.Steep(x, z, true) {
				steep = append(steep, sample{nav.Point{X: x, Z: z}, geo.Terrain.Top(x, z)})
			}
		}
	}
	g := m.Grid()
	starts := []nav.Point{mapfile.Point(m.PlayerSpawn.At)}
	for _, pt := range m.Portals {
		starts = append(starts, mapfile.Point(pt.At))
	}
	player := g.Reach(starts, p.Radius, nav.JumpRise(p))
	type home struct {
		mob   string
		at    nav.Point
		leash float64
	}
	var homes []home
	for _, s := range m.Spawns {
		homes = append(homes, home{s.Mob, mapfile.Point(s.At), s.Radius + data.MobRules().Leash})
	}
	if b := m.Boss; b != nil {
		homes = append(homes, home{b.Mob, mapfile.Point(b.At), b.Radius + data.MobRules().Leash})
	}
	var out []gap
	for _, s := range solids(m, p) {
		type near struct {
			at    nav.Point
			width float64
		}
		var by []near
		for _, w := range steep {
			if w.top <= s.base+p.Step {
				continue
			}
			if d := nav.Distance(w.at, s.nearest(w.at)) - half; d <= gapReach && open(geo.Terrain, s.nearest(w.at), w.at, d) {
				by = append(by, near{w.at, max(d, 0)})
			}
		}
		var narrowest []near
		for _, w := range by {
			if slices.ContainsFunc(by, func(v near) bool { return v.width < w.width && nav.Distance(v.at, w.at) <= 1 }) ||
				slices.ContainsFunc(narrowest, func(v near) bool { return nav.Distance(v.at, w.at) <= 1 }) {
				continue
			}
			narrowest = append(narrowest, w)
		}
		for _, w := range narrowest {
			if w.width < 2*p.Radius {
				continue // too narrow for a player
			}
			edge := s.nearest(w.at)
			mid := nav.Point{X: (w.at.X + edge.X) / 2, Z: (w.at.Z + edge.Z) / 2}
			if !player.Has(mid) {
				continue
			}
			widest := gap{what: s.what, width: w.width, middle: mid}
			for _, h := range homes {
				if nav.Distance(mid, h.at) > h.leash {
					continue
				}
				if def, ok := data.Mob(h.mob); ok && 2*def.Radius > widest.across {
					widest.mob, widest.across = h.mob, 2*def.Radius
				}
			}
			if w.width < widest.across {
				out = append(out, widest)
			}
		}
	}
	return out
}

// open reports whether the first d metres from a toward b cross only ground that isn't steep, so a gap runs over open floor, not along the face of a bank the solid is set into.
func open(t sim.Terrain, a, b nav.Point, d float64) bool {
	length := nav.Distance(a, b)
	for s := 0.0; s < d && length > 0; s += 0.1 {
		x, z := a.X+(b.X-a.X)*s/length, a.Z+(b.Z-a.Z)*s/length
		if t.Steep(x, z, false) || t.Steep(x, z, true) {
			return false
		}
	}
	return true
}

// On every designed hunting map, no solid leaves a gap to terrain too steep to walk that a player fits and the widest mob whose leash reaches it doesn't. The sim tests terrain at a body's centre, so a mob chasing into such a gap sinks into the bank, which the mob reach test can't see.
func TestNoSolidLeavesAGapByTheTerrainOnlyAPlayerFits(t *testing.T) {
	data := gamedatatest.Shipped(t)
	d := shipped(t)
	for _, m := range d.maps {
		if m.Kind != "hunting" || slices.ContainsFunc(undesigned, func(u struct{ mapID, why string }) bool { return u.mapID == m.ID }) {
			continue
		}
		t.Run(m.ID, func(t *testing.T) {
			for _, g := range terrainGaps(t, data, m, d.params) {
				t.Error(g)
			}
		})
	}
}

// gapFixture is a strip between two banks with a snow weasel, 2.625 m across, roaming it: one boulder 1.98 m from the north bank, which only a player fits, one 3.2 m from it, and a pine hugging it.
const gapFixture = `
name: Gaps
kind: hunting
town: town
levels: [1, 1]
look: {ground: snow, sky: snow}
size: [30, 20]
terrain:
  cell: 0.5
  mesas:
    - {area: [-5, 17, 40, 10], height: 3, edge: 0.5}
    - {area: [-5, -7, 40, 10], height: 3, edge: 0.5}
solids:
  - {cylinder: [8, 12], radius: 2.52, height: 2.31, model: boulder}
  - {cylinder: [22, 10.78], radius: 2.52, height: 2.31, model: boulder}
  - {cylinder: [15, 16], radius: 0.34, height: 6.5, model: tree_pine}
portals:
  - {name: west, at: [2, 10], radius: 1.2, to: {map: town, portal: to_forest}}
spawns:
  - {name: weasel, at: [15, 9], radius: 3, mob: snow_weasel, max: 1, respawn: 12s}
player_spawn: {at: [3.5, 10], yaw: 90}
`

// The gap check finds the one gap a player fits and the weasel doesn't, and passes the wide gap and the hug.
func TestTheTerrainGapCheckFindsTheGapOnlyAPlayerFits(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := shipped(t).params
	var f mapfile.File
	if err := yaml.Unmarshal([]byte(gapFixture), &f); err != nil {
		t.Fatal(err)
	}
	m, err := mapfile.Build("gaps", f, p)
	if err != nil {
		t.Fatal(err)
	}
	got := terrainGaps(t, data, m, p)
	if len(got) != 1 {
		t.Fatalf("found %d gaps, want the one by the boulder at (8, 12): %v", len(got), got)
	}
	if g := got[0]; g.what != "the cylinder at (8, 12)" || math.Abs(g.width-1.98) > 0.01 || g.mob != "snow_weasel" {
		t.Errorf("found %v, want the boulder at (8, 12), 1.98 m from the bank, under the snow_weasel", g)
	}
}
