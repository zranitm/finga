package regiongen

import (
	"math"
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
