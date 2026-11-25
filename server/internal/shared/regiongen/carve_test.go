package regiongen

import (
	"math"
	"slices"
	"testing"
)

// cave makes p a cave: its sky, and rock 4.5 m over its ground.
func cave(p *Plan) {
	p.Look.Sky, p.Rock = Cave, &Rock{Height: 4.5}
}

// colourAt is the kit ground of sample (x, z).
func (r *Region) colourAt(x, z int) string {
	return r.Palette[r.Colours[z*r.Geometry.Terrain.NX+x]]
}

// A hall is carved back down to its ground, its walls rising over two samples, too steep to walk, to the rock that stands over the rest; the rock and its walls are cave_rock, the floor its area's colours, and nothing is scattered on the rock.
func TestAHallIsCarvedToItsFloorBetweenSteepWalls(t *testing.T) {
	p := flat()
	cave(&p)
	p.Features = []Feature{{Hall: &Hall{Area: [][2]float64{{40, 40}, {88, 40}, {88, 88}, {40, 88}}}}}
	p.Biomes["meadow"] = Biome{Colours: p.Biomes["meadow"].Colours, Scatter: []Scatter{{Model: "rock_small", Spacing: 3}}}
	r := build(t, p)
	near(t, "the hall's floor", r.at(64, 64), 0)
	near(t, "the rock", r.at(20, 20), 4.5)
	near(t, "the wall a sample out", r.at(89, 64), 2.25)
	near(t, "the wall's top", r.at(90, 64), 4.5)
	if !r.steepBetween(88, 64, false) || !r.steepBetween(89, 64, false) {
		t.Error("the hall's east wall is walkable")
	}
	if got := []string{r.colourAt(64, 64), r.colourAt(89, 64), r.colourAt(20, 20)}; got[0] != "grass" || got[1] != rockColour || got[2] != rockColour {
		t.Errorf("the floor, the wall and the rock are %v; want grass, then cave_rock twice", got)
	}
	if len(r.Props) == 0 {
		t.Fatal("nothing scattered in the hall")
	}
	for _, pr := range r.Props {
		if s := r.Colours[int(math.Round(pr.Z))*r.Geometry.Terrain.NX+int(math.Round(pr.X))]; r.Palette[s] == rockColour {
			t.Fatalf("a %s at (%v, %v), on the rock", r.Models[pr.Model], pr.X, pr.Z)
		}
	}
}

// A carved wall's line wanders by noise, up to the rock's rough either way, so a hall is no polygon.
func TestACarvedWallsLineWanders(t *testing.T) {
	p := flat()
	cave(&p)
	p.Rock.Rough = 1.5
	p.Features = []Feature{{Hall: &Hall{Area: [][2]float64{{40, 40}, {88, 40}, {88, 88}, {40, 88}}}}}
	r := build(t, p)
	lo, hi := math.MaxInt, math.MinInt
	for z := 44; z <= 84; z++ {
		x := 64
		for r.colourAt(x, z) != rockColour {
			x++
		}
		lo, hi = min(lo, x), max(hi, x)
	}
	if lo == hi || lo < 86 || hi > 90 {
		t.Errorf("the east wall's rock starts from x = %d to %d; want it wandering within 1.5 m of x = 88", lo, hi)
	}
}

// A flat hall's floor is level, at its ground's mean; any other follows its ground.
func TestAFlatHallIsLevel(t *testing.T) {
	p := flat()
	cave(&p)
	p.Ground.Noise = Noise{Scale: 30, Amplitude: 3, Octaves: 2}
	p.Features = []Feature{
		{Hall: &Hall{Area: [][2]float64{{12, 12}, {56, 12}, {56, 116}, {12, 116}}, Flat: true}},
		{Hall: &Hall{Area: [][2]float64{{72, 12}, {116, 12}, {116, 116}, {72, 116}}}},
	}
	r := build(t, p)
	spread := func(x0, x1 int) float64 {
		lo, hi := math.Inf(1), math.Inf(-1)
		for z := 20; z <= 108; z++ {
			for x := x0; x <= x1; x++ {
				lo, hi = min(lo, r.at(x, z)), max(hi, r.at(x, z))
			}
		}
		return hi - lo
	}
	if flat, follows := spread(20, 48), spread(80, 108); flat != 0 || follows < 0.5 {
		t.Errorf("the flat hall's floor spans %v m and the other's %v; want 0, and the ground's rise and fall", flat, follows)
	}
}

// A tunnel is carved its width across along its path, its floor levelled along it as a road's is, gentler than the ground a hall follows.
func TestATunnelIsCarvedItsWidthAndLevelled(t *testing.T) {
	strip := func(f Feature) *Region {
		p := flat()
		cave(&p)
		p.Ground.Noise = Noise{Scale: 30, Amplitude: 3, Octaves: 2}
		p.Features = []Feature{f}
		return build(t, p)
	}
	tunnel := strip(Feature{Tunnel: &Tunnel{Path: [][2]float64{{16, 64}, {112, 64}}, Width: 6}})
	hall := strip(Feature{Hall: &Hall{Area: [][2]float64{{16, 61}, {112, 61}, {112, 67}, {16, 67}}}})
	steepest := func(r *Region) float64 {
		most := 0.0
		for x := 30; x < 98; x++ {
			most = max(most, math.Abs(r.at(x+1, 64)-r.at(x, 64)))
		}
		return most
	}
	if lv, gr := steepest(tunnel), steepest(hall); lv >= gr {
		t.Errorf("the tunnel's floor steps up to %v m a metre, the ground's %v; want it levelled", lv, gr)
	}
	for _, x := range []int{30, 64, 97} {
		if tunnel.colourAt(x, 67) == rockColour || tunnel.colourAt(x, 61) == rockColour || tunnel.colourAt(x, 69) != rockColour || tunnel.colourAt(x, 59) != rockColour {
			t.Errorf("at x = %d the tunnel isn't carved 3 m each side of its path, rock 2 m past that", x)
		}
	}
}

// The test cave stands its gate, waypoint, start, camp and boss on carved floor: a plan's spots in a cave are where it's carved.
func TestTheTestCavesSpotsAreCarved(t *testing.T) {
	p := plan(t, "testdata/cave.yaml")
	r := build(t, p)
	spots := []spot{{what: "the camp", at: p.Camps[0].At}, {what: "the boss", at: p.Areas[0].Boss.At}}
	for _, s := range append(spots, p.spots()...) {
		if c := r.colourAt(int(s.at[0]/p.Cell), int(s.at[1]/p.Cell)); c == rockColour {
			t.Errorf("%s at %v stands on the rock", s.what, s.at)
		}
	}
}

// The test cave's halls have no ground too steep to walk away from their walls: a tunnel joining one carves no pit or trench into it.
func TestTheTestCavesHallsHaveNoSteepGround(t *testing.T) {
	p := plan(t, "testdata/cave.yaml")
	r := build(t, p)
	g := grid{nx: samples(p.Size[0], p.Cell), nz: samples(p.Size[1], p.Cell), cell: p.Cell}
	keep := p.wallReach() + p.Cell
	for i, f := range p.Features {
		if f.Hall == nil {
			continue
		}
		area := f.Hall.Area
		g.fill(area, func(s int) {
			x, z := g.x(s%g.nx), g.x(s/g.nx)
			for k, a := range area {
				if d2, _ := segment(x, z, a, area[(k+1)%len(area)]); d2 < float64(keep*keep) {
					return
				}
			}
			mx, mz := x+p.Cell/2, z+p.Cell/2
			if r.Geometry.Terrain.Steep(mx, mz, false) || r.Geometry.Terrain.Steep(mx, mz, true) {
				t.Errorf("feature %d: the hall's ground is steep at (%g, %g)", i+1, mx, mz)
			}
		})
	}
}

// steepCells are the cells whose middles keep passes and are steep along x or z.
func steepCells(r *Region, keep func(x, z float64) bool) [][2]float64 {
	t := r.Geometry.Terrain
	var steep [][2]float64
	for iz := range t.NZ - 1 {
		for ix := range t.NX - 1 {
			x, z := float64(float64(ix)+0.5)*t.Cell, float64(float64(iz)+0.5)*t.Cell
			if keep(x, z) && (t.Steep(x, z, false) || t.Steep(x, z, true)) {
				steep = append(steep, [2]float64{x, z})
			}
		}
	}
	return steep
}

// A tunnel's floor joins the hall it starts in and the tunnel it branches from, though the ground's mean along it misses their floors: no pit or step in either. Without the floor rule, seed 10's tunnel digs into the hall; without the join, seed 28's branch steps down into the tunnel it leaves.
func TestATunnelJoinsTheSpacesItMeets(t *testing.T) {
	for _, seed := range []uint32{10, 28} {
		p := flat()
		cave(&p)
		p.Seed, p.Rock.Rough = seed, 1
		p.Ground.Noise = Noise{Scale: 30, Amplitude: 3, Octaves: 2}
		hall := [][2]float64{{16, 40}, {48, 40}, {48, 88}, {16, 88}}
		p.Features = []Feature{{Hall: &Hall{Area: hall}}, {Tunnel: &Tunnel{Path: [][2]float64{{40, 64}, {108, 64}}, Width: 8}}, {Tunnel: &Tunnel{Path: [][2]float64{{80, 64}, {80, 104}}, Width: 8}}}
		r := build(t, p)
		keep := p.Rock.Rough + p.Cell // a wall's foot wanders in this far
		for what, in := range map[string]func(x, z float64) bool{
			"the hall": func(x, z float64) bool {
				return contains(hall, [2]float64{x, z}) && !onPath(append(slices.Clone(hall), hall[0]), [2]float64{x, z}, keep)
			},
			"the tunnel, from its start in the hall": func(x, z float64) bool { return x > 40 && x < 108-4 && math.Abs(z-64) < 4-keep },
			"the branch":                             func(x, z float64) bool { return z > 64 && z < 104-4 && math.Abs(x-80) < 4-keep },
		} {
			if got := steepCells(r, in); len(got) > 0 {
				t.Errorf("seed %d: %s is steep away from its walls at %v", seed, what, got)
			}
		}
	}
}

// join holds a tunnel's floor to a flat hall's and an earlier tunnel's where its path runs through them, and between them keeps it within half the walkable slope of theirs.
func TestJoinHoldsATunnelToTheFloorsItMeets(t *testing.T) {
	b := &builder{
		p:        Plan{Features: []Feature{{Hall: &Hall{Area: [][2]float64{{0, -5}, {10, -5}, {10, 5}, {0, 5}}, Flat: true}}}},
		maxSlope: 0.7,
		flat:     []float64{2},
		tunnels:  []carved{{path: [][2]float64{{60, -10}, {60, 10}}, levels: []float64{9, 9}, half: 3}},
	}
	path := [][2]float64{{5, 0}, {15, 0}, {25, 0}, {35, 0}, {45, 0}, {55, 0}, {60, 0}}
	levels := make([]float64, len(path))
	b.join(path, along(path), levels)
	want := []float64{2, 0, 0, 0.25, 3.75, 7.25, 9} // the hall's 2, the ground's 0 where both ramps allow it, then 0.35 a metre down from the tunnel's 9
	for i := range want {
		if math.Abs(levels[i]-want[i]) > 1e-9 {
			t.Fatalf("levels %v; want %v", levels, want)
		}
	}
	b.tunnels = append(b.tunnels, carved{path: [][2]float64{{17, -10}, {17, 10}}, levels: []float64{9, 9}, half: 3})
	short := [][2]float64{{5, 0}, {11, 0}, {17, 0}} // floors 7 m apart over 12 m, past the ramp's 0.35 a metre
	levels = make([]float64, len(short))
	b.join(short, along(short), levels)
	if math.Abs(levels[1]-5.5) > 1e-9 {
		t.Errorf("between joins too far apart for the ramp: %v; want 5.5, halfway between the hall's 2 + 2.1 and the tunnel's 9 − 2.1", levels[1])
	}
}

// A cave's rock is deep water's mark, a wall at any height, from a body's radius before its wall's foot.
func TestACavesRockIsAWallFromABodysRadiusBeforeItsFoot(t *testing.T) {
	p := flat()
	cave(&p)
	p.Cell = 0.25
	p.Features = []Feature{{Hall: &Hall{Area: [][2]float64{{40, 40}, {88, 40}, {88, 88}, {40, 88}}}}}
	r := build(t, p)
	tr := r.Geometry.Terrain
	walled := func(x float64) bool { return tr.DeepWater[int(64/p.Cell)*tr.NX+int(x/p.Cell)] }
	for x, want := range map[float64]bool{30: true, 39.75: true, 40: true, 40.25: true, 40.5: false, 64: false} { // the body's 0.375 m
		if walled(x) != want {
			t.Errorf("the sample at x = %g is marked %v; want %v", x, walled(x), want)
		}
	}
}
