package regiongen

import (
	"math"
	"strings"
	"testing"
)

// A plan the generator can't build is refused, saying why.
func TestABadPlanIsRefused(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(p *Plan)
		want string
	}{
		{"no size", func(p *Plan) { p.Size[0] = 0 }, "whole 128 m map tiles"},
		{"a side past 8 km", func(p *Plan) { p.Size[0] = 8320 }, "up to 8192 m"},
		{"a cell not a power of two", func(p *Plan) { p.Cell = 8.0 / 3 }, "a power of two"},
		{"a cell that doesn't divide it", func(p *Plan) { p.Cell = 0.3 }, "a power of two"},
		{"part of a chunk", func(p *Plan) { p.Size = [2]float64{160, 128} }, "whole 128 m map tiles"},
		{"part of a map tile", func(p *Plan) { p.Size = [2]float64{192, 128} }, "whole 128 m map tiles"},
		{"a cell past the overview's", func(p *Plan) { p.Cell = 16 }, "a power of two"},
		{"too many samples", func(p *Plan) { p.Size, p.Cell = [2]float64{4096, 4096}, 0.25 }, "2²⁴ terrain samples"},
		{"no sky", func(p *Plan) { p.Look.Sky = "" }, "a sky"},
		{"no octaves", func(p *Plan) { p.Ground.Noise.Octaves = 0 }, "0 octaves; 1 to 8"},
		{"noise finer than a cell", func(p *Plan) { p.Ground.Noise = Noise{Scale: 6, Amplitude: 1, Octaves: 4} }, "at least a cell across"},
		{"a mountain of an amplitude", func(p *Plan) { p.Ground.Noise.Amplitude = 1e300 }, "0 to 1000 m"},
		{"an edge wider than half the region", func(p *Plan) { p.Edge.Width = 132 }, "half the region's shorter side"},
		{"an edge off the area squares", func(p *Plan) { p.Edge.Width = 10 }, "whole 4 m area squares"},
		{"edge heights high to low", func(p *Plan) { p.Edge.Height = [2]float64{9, 3} }, "low to high"},
		{"an edge biome that isn't one", func(p *Plan) { p.Edge.Biome = "moon" }, `no biome "moon"`},
		{"no colour slope", func(p *Plan) { p.Colour.Slope = 0 }, "positive slope"},
		{"a colour not in the kit", func(p *Plan) { p.Biomes["meadow"] = Biome{Colours: Colours{"grass", "dirt", "lava", "snow"}} }, `"lava"`},
		{"a scatter model not in the kit", func(p *Plan) {
			p.Biomes["meadow"] = Biome{Colours: flat().Biomes["meadow"].Colours, Scatter: []Scatter{{Model: "teapot", Spacing: 3}}}
		}, `"teapot"`},
		{"a scatter without spacing", func(p *Plan) {
			p.Biomes["meadow"] = Biome{Colours: flat().Biomes["meadow"].Colours, Scatter: []Scatter{{Model: "tree_oak"}}}
		}, "a spacing of 1 to 512 m"},
		{"a scatter a millimetre apart", func(p *Plan) {
			p.Biomes["meadow"] = Biome{Colours: flat().Biomes["meadow"].Colours, Scatter: []Scatter{{Model: "tree_oak", Spacing: 0.001}}}
		}, "a spacing of 1 to 512 m"},
		{"an endless away", func(p *Plan) {
			p.Biomes["meadow"] = Biome{Colours: flat().Biomes["meadow"].Colours, Scatter: []Scatter{{Model: "tree_oak", Spacing: 4, Away: Away{Road: 1e9}}}}
		}, "away distances of 0 to 512 m"},
		{"two features in one", func(p *Plan) {
			p.Features = []Feature{{Ridge: &Ridge{Path: [][2]float64{{1, 1}, {2, 2}}, Width: 1, Height: 1}, Road: &Road{Path: [][2]float64{{1, 1}, {2, 2}}, Width: 1}}}
		}, "one of a ridge"},
		{"a one-point path", func(p *Plan) { p.Features = []Feature{{Road: &Road{Path: [][2]float64{{1, 1}}, Width: 1}}} }, "a road needs a path"},
		{"a path off the region", func(p *Plan) { p.Features = []Feature{{Road: &Road{Path: [][2]float64{{1, 1}, {500, 9}}, Width: 1}}} }, "a road needs a path in the region"},
		{"a ford off its river", func(p *Plan) {
			p.Features = []Feature{{River: &River{Path: [][2]float64{{40, 0}, {40, 128}}, Width: 6, Depth: 2, Fords: [][2]float64{{40, 30}, {43.5, 60}}}}}
		}, "ford at [43.5 60] is off it"},
		{"a river starting short of the ring", func(p *Plan) {
			p.Features = []Feature{{River: &River{Path: [][2]float64{{40, 4}, {40, 128}}, Width: 6, Depth: 2}}}
		}, "end at [40 4] is 0 m inside the ring's face"},
		{"a river ending short of the ring", func(p *Plan) {
			p.Features = []Feature{{River: &River{Path: [][2]float64{{40, 0}, {40, 100}}, Width: 6, Depth: 2}}}
		}, "end at [40 100] is 24 m inside the ring's face"},
		{"an endless ridge", func(p *Plan) {
			p.Features = []Feature{{Ridge: &Ridge{Path: [][2]float64{{1, 1}, {9, 9}}, Width: math.Inf(1), Height: 1}}}
		}, "a width up to 512 m"},
		{"a plateau of no height", func(p *Plan) {
			p.Features = []Feature{{Plateau: &Plateau{Area: [][2]float64{{1, 1}, {9, 1}, {9, 9}}, Edge: Ramp}}}
		}, "a height other than 0"},
		{"a plateau's edge neither", func(p *Plan) {
			p.Features = []Feature{{Plateau: &Plateau{Area: [][2]float64{{1, 1}, {9, 1}, {9, 9}}, Height: 1, Edge: "wall"}}}
		}, "a cliff or ramp edge"},
		{"a dry river", func(p *Plan) { p.Features = []Feature{{River: &River{Path: [][2]float64{{1, 1}, {9, 9}}, Width: 4}}} }, "a depth up to 1000 m"},
		{"a ford off the region", func(p *Plan) {
			p.Features = []Feature{{River: &River{Path: [][2]float64{{1, 1}, {9, 9}}, Width: 4, Depth: 1, Fords: [][2]float64{{-5, 5}}}}}
		}, "fords in the region"},
		{"too many areas", func(p *Plan) { p.Areas = make([]Area, maxAreas+1) }, "at most 32767"},
		{"a boss off the region", func(p *Plan) { p.Areas[0].Boss = &Boss{At: [2]float64{5, 300}, Radius: 4} }, "its boss needs a spot in the region"},
		{"a two-corner area", func(p *Plan) { p.Areas[0].Area = p.Areas[0].Area[:2] }, "needs an id and an area"},
		{"an area of no kind", func(p *Plan) { p.Areas[0].Kind = "lair" }, "hunting or town"},
		{"a camp off the region", func(p *Plan) { p.Camps = []Camp{{Name: "far", At: [2]float64{200, 5}, Radius: 3}} }, "a spot in the region"},
		{"an endless camp", func(p *Plan) { p.Camps = []Camp{{Name: "wide", At: [2]float64{20, 5}, Radius: math.Inf(1)}} }, "a radius up to 512 m"},
		{"an endless gate", func(p *Plan) { p.Gates = []Gate{{Name: "east", At: [2]float64{20, 5}, Radius: math.Inf(1)}} }, "gate 1 (east): needs a radius up to 512 m"},
		{"an npc facing nowhere", func(p *Plan) { p.NPCs = []NPC{{ID: "guide", At: [2]float64{20, 5}, Yaw: math.NaN()}} }, "npc 1 (guide): needs a yaw"},
		{"a start facing nowhere", func(p *Plan) { p.Start.Yaw = math.Inf(-1) }, "the start needs a yaw"},
		{"a house over the edge", func(p *Plan) {
			p.Pieces = []Piece{{House: &House{Area: [4]float64{120, 5, 20, 5}, Height: 3, Model: "shop"}}}
		}, "an area in the region"},
		{"a piece that's both", func(p *Plan) {
			p.Pieces = []Piece{{Model: "well", At: &[2]float64{5, 5}, House: &House{Area: [4]float64{5, 5, 2, 2}, Height: 3, Model: "shop"}}}
		}, "either a model at a spot or a house"},
		{"a piece not in the kit", func(p *Plan) { p.Pieces = []Piece{{Model: "teapot", At: &[2]float64{5, 5}}} }, "a kit model"},
		{"a waypoint off the region", func(p *Plan) { p.Waypoints = []Waypoint{{ID: "far", At: [2]float64{5, -1}}} }, "waypoint far at [5 -1]: outside the region"},
		{"a start off the region", func(p *Plan) { p.Start.At = [2]float64{-3, 5} }, "the start at [-3 5]: outside the region"},
		{"rock on a sea", func(p *Plan) { p.Look.Sky, p.Rock = Sea, &Rock{Height: 4.5} }, "rock is a cave's: a sky of cave, not sea"},
		{"rock of no height", func(p *Plan) { cave(p); p.Rock.Height = 0 }, "rock needs a height up to 1000 m"},
		{"an endless rough", func(p *Plan) { cave(p); p.Rock.Rough = math.Inf(1) }, "a rough of 0 to 512 m"},
		{"a hall with no rock", func(p *Plan) { p.Features = []Feature{{Hall: &Hall{Area: [][2]float64{{40, 40}, {80, 40}, {80, 80}}}}} }, "carved from a cave's rock"},
		{"a hall into the ring", func(p *Plan) {
			cave(p)
			p.Features = []Feature{{Hall: &Hall{Area: [][2]float64{{5, 40}, {80, 40}, {80, 80}}}}}
		}, "a hall needs an area in the region, its walls clear of the ring: at least 2 m inside the ring's face"},
		{"a tunnel into the ring", func(p *Plan) {
			cave(p)
			p.Features = []Feature{{Tunnel: &Tunnel{Path: [][2]float64{{64, 64}, {120, 64}}, Width: 6}}}
		}, "a tunnel needs a width up to 512 m and a path in the region, its walls clear of the ring: at least 5 m inside the ring's face"},
		{"a tunnel of no width", func(p *Plan) {
			cave(p)
			p.Features = []Feature{{Tunnel: &Tunnel{Path: [][2]float64{{40, 64}, {80, 64}}}}}
		}, "a tunnel needs a width"},
	} {
		p := flat()
		c.edit(&p)
		_, err := Generate(p, shippedKit(t), maxSlope(t))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v; want one saying %q", c.name, err, c.want)
		}
	}
	k := shippedKit(t)
	delete(k.Grounds, roadColour)
	if _, err := Generate(flat(), k, maxSlope(t)); err == nil || !strings.Contains(err.Error(), "needs a dirt ground") {
		t.Errorf("a kit without dirt: error %v; want one saying it needs a dirt ground", err)
	}
	k = shippedKit(t)
	delete(k.Grounds, rockColour)
	p := flat()
	cave(&p)
	if _, err := Generate(p, k, maxSlope(t)); err == nil || !strings.Contains(err.Error(), "needs a cave_rock ground") {
		t.Errorf("a cave's kit without cave_rock: error %v; want one saying it needs a cave_rock ground", err)
	}
}

// A plan's unknown key is an error, as every data file's is.
func TestAPlansUnknownKeyIsRefused(t *testing.T) {
	if _, err := Decode([]byte("name: x\nsize: [64, 64]\nhills: []\n")); err == nil || !strings.Contains(err.Error(), "hills") {
		t.Errorf("error %v; want one naming hills", err)
	}
}

// A kit is refused for a colour not in #rrggbb, a model both a cylinder and a box, a solid of no size, or an unknown key.
func TestABadKitIsRefused(t *testing.T) {
	for _, c := range []struct{ kit, want string }{
		{"grounds: {}\nmodels: {}\n", "1 to 256 grounds"},
		{"grounds: {grass: '#8CC265'}\n", "#rrggbb in lower case"},
		{"grounds: {grass: '#8cc265'}\nmodels: {rock: {cylinder: {radius: 1, height: 1}, box: {width: 1, depth: 1, height: 1}}}\n", "not both"},
		{"grounds: {grass: '#8cc265'}\nmodels: {rock: {cylinder: {radius: 0, height: 1}}}\n", "a radius up to 512 m"},
		{"grounds: {grass: '#8cc265'}\nmodels: {rock: {cylinder: {radius: 1e300, height: 1}}}\n", "a radius up to 512 m"},
		{"grounds: {grass: '#8cc265'}\nmodels: {hut: {box: {width: 2, depth: 2}}}\n", "a height up to 1000 m"},
		{"grounds: {grass: '#8cc265'}\nsizes: {}\n", "sizes"},
	} {
		if _, err := DecodeKit([]byte(c.kit)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("kit %q: error %v; want one saying %q", c.kit, err, c.want)
		}
	}
}
