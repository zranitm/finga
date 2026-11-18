package regiongen

import (
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
		{"no size", func(p *Plan) { p.Size[0] = 0 }, "both sides must be positive"},
		{"a cell that doesn't divide it", func(p *Plan) { p.Cell = 0.3 }, "divide the size"},
		{"part of a chunk", func(p *Plan) { p.Size = [2]float64{160, 128} }, "whole 128 m map tiles"},
		{"part of a map tile", func(p *Plan) { p.Size = [2]float64{192, 128} }, "whole 128 m map tiles"},
		{"a cell that doesn't divide the overview", func(p *Plan) { p.Cell = 3 }, "divide"},
		{"no sky", func(p *Plan) { p.Look.Sky = "" }, "a sky"},
		{"no octaves", func(p *Plan) { p.Ground.Noise.Octaves = 0 }, "1 to 8 octaves"},
		{"edge heights high to low", func(p *Plan) { p.Edge.Height = [2]float64{9, 3} }, "low to high"},
		{"an edge biome that isn't one", func(p *Plan) { p.Edge.Biome = "moon" }, `no biome "moon"`},
		{"no colour slope", func(p *Plan) { p.Colour.Slope = 0 }, "positive slope"},
		{"a colour not in the kit", func(p *Plan) { p.Biomes["meadow"] = Biome{Colours: Colours{"grass", "dirt", "lava", "snow"}} }, `"lava"`},
		{"a scatter model not in the kit", func(p *Plan) {
			p.Biomes["meadow"] = Biome{Colours: flat().Biomes["meadow"].Colours, Scatter: []Scatter{{Model: "teapot", Spacing: 3}}}
		}, `"teapot"`},
		{"a scatter without spacing", func(p *Plan) {
			p.Biomes["meadow"] = Biome{Colours: flat().Biomes["meadow"].Colours, Scatter: []Scatter{{Model: "tree_oak"}}}
		}, "positive spacing"},
		{"two features in one", func(p *Plan) {
			p.Features = []Feature{{Ridge: &Ridge{Path: [][2]float64{{1, 1}, {2, 2}}, Width: 1, Height: 1}, Road: &Road{Path: [][2]float64{{1, 1}, {2, 2}}, Width: 1}}}
		}, "one of a ridge"},
		{"a one-point path", func(p *Plan) { p.Features = []Feature{{Road: &Road{Path: [][2]float64{{1, 1}}, Width: 1}}} }, "a road needs a path"},
		{"a plateau's edge neither", func(p *Plan) {
			p.Features = []Feature{{Plateau: &Plateau{Area: [][2]float64{{1, 1}, {9, 1}, {9, 9}}, Height: 1, Edge: "wall"}}}
		}, "a cliff or ramp edge"},
		{"a dry river", func(p *Plan) { p.Features = []Feature{{River: &River{Path: [][2]float64{{1, 1}, {9, 9}}, Width: 4}}} }, "a positive depth"},
		{"a two-corner area", func(p *Plan) { p.Areas[0].Area = p.Areas[0].Area[:2] }, "needs an id and an area"},
		{"an area of no kind", func(p *Plan) { p.Areas[0].Kind = "lair" }, "hunting or town"},
		{"a camp off the region", func(p *Plan) { p.Camps = []Camp{{Name: "far", At: [2]float64{200, 5}, Radius: 3}} }, "a spot in the region"},
		{"a piece that's both", func(p *Plan) {
			p.Pieces = []Piece{{Model: "well", At: &[2]float64{5, 5}, House: &House{Area: [4]float64{5, 5, 2, 2}, Height: 3, Model: "shop"}}}
		}, "either a model at a spot or a house"},
		{"a piece not in the kit", func(p *Plan) { p.Pieces = []Piece{{Model: "teapot", At: &[2]float64{5, 5}}} }, "a kit model"},
		{"a waypoint off the region", func(p *Plan) { p.Waypoints = []Waypoint{{ID: "far", At: [2]float64{5, -1}}} }, "waypoint far at [5 -1]: outside the region"},
		{"a start off the region", func(p *Plan) { p.Start.At = [2]float64{-3, 5} }, "the start at [-3 5]: outside the region"},
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
		{"grounds: {grass: '#8cc265'}\nmodels: {rock: {cylinder: {radius: 0, height: 1}}}\n", "positive radius"},
		{"grounds: {grass: '#8cc265'}\nmodels: {hut: {box: {width: 2, depth: 2}}}\n", "positive width, depth and height"},
		{"grounds: {grass: '#8cc265'}\nsizes: {}\n", "sizes"},
	} {
		if _, err := DecodeKit([]byte(c.kit)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("kit %q: error %v; want one saying %q", c.kit, err, c.want)
		}
	}
}
