package regiongen

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"

	"finga/internal/shared/sim"
)

// shippedKit is data/kit.yaml.
func shippedKit(t testing.TB) Kit {
	t.Helper()
	raw, err := os.ReadFile("../../../data/kit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	k, err := DecodeKit(raw)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// body is movement.yaml's slope and radius.
func body(t testing.TB) sim.Params {
	t.Helper()
	raw, err := os.ReadFile("../../../data/movement.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		MaxSlope float64 `yaml:"max_slope"`
		Radius   float64 `yaml:"radius"`
	}
	if err := yaml.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return sim.Params{MaxSlope: m.MaxSlope, Radius: m.Radius}
}

// flat is a 128 m region of level ground at 0, one meadow over it and nothing scattered: each test adds what it builds.
func flat() Plan {
	square := [][2]float64{{0, 0}, {128, 0}, {128, 128}, {0, 128}}
	return Plan{
		Name: "Flat", Size: [2]float64{128, 128}, Cell: 1, Seed: 1, Look: Look{Sky: "day"},
		Ground: Ground{Noise: Noise{Scale: 50, Amplitude: 0, Octaves: 1}},
		Edge:   Edge{Width: AreaCell, Biome: "meadow"},
		Colour: Colour{Slope: 0.25, High: 100},
		Areas:  []Area{{ID: "all", Area: square, Biome: "meadow", Kind: Hunting}},
		Start:  Place{At: [2]float64{2, 2}},
		Biomes: map[string]Biome{"meadow": {Colours: Colours{Flat: "grass", Slope: "dirt", Steep: "stone", High: "snow"}}},
	}
}

func build(t testing.TB, p Plan) *Region {
	t.Helper()
	r, err := Generate(p, shippedKit(t), body(t))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// at is the height of the sample at (x, z), whole metres.
func (r *Region) at(x, z int) float64 {
	t := r.Geometry.Terrain
	return t.Heights[z*t.NX+x]
}

// steepBetween is whether the cell from sample (x, z) to (x+1, z), or to (x, z+1) when alongZ, is marked steep.
func (r *Region) steepBetween(x, z int, alongZ bool) bool {
	return r.Geometry.Terrain.Steep(float64(x)+0.5, float64(z)+0.5, alongZ)
}

func near(t *testing.T, what string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-5 {
		t.Errorf("%s: %v; want %v", what, got, want)
	}
}

// The ring of mountains rises over everything inside it: its inner face a cliff edgeCliff over the highest ground inside, too steep to walk, then up by its heights toward the sides; a river and a ridge reaching into it neither cut nor raise it.
func TestTheRingIsACliffOverTheHighestGroundInside(t *testing.T) {
	p := flat()
	p.Edge = Edge{Width: 12, Height: [2]float64{4, 4}, Biome: "meadow"}
	p.Features = []Feature{
		{Plateau: &Plateau{Area: [][2]float64{{50, 50}, {70, 50}, {70, 70}, {50, 70}}, Height: 5, Edge: Cliff}},
		{River: &River{Path: [][2]float64{{0, 30}, {125, 30}}, Width: 6, Depth: 2}},
		{Ridge: &Ridge{Path: [][2]float64{{64, 100}, {125, 100}}, Width: 10, Height: 3}},
	}
	r := build(t, p)
	near(t, "the plateau, the highest ground inside", r.at(60, 60), 5)
	for z := range 129 {
		for x := range 129 {
			d := min(x, z, 128-x, 128-z)
			if d >= 12 {
				continue
			}
			want := f32(5 + edgeCliff + float64(smoothstep(1-float64(d)/12)*4))
			if got := r.at(x, z); got != want {
				t.Fatalf("the ring at (%d, %d), %d m in: %v; want %v", x, z, 12-d, got, want)
			}
			if r.Wet[z*129+x] {
				t.Fatalf("the ring at (%d, %d) is under water", x, z)
			}
		}
	}
	for _, z := range []int{30, 64, 100} {
		if !r.steepBetween(116, z, false) || !r.steepBetween(11, z, false) {
			t.Errorf("the inner face by z = %d isn't steep both sides", z)
		}
	}
}

// Deep water is water more than wadeDepth over the final ground (large-regions.md decision 9): a river's channel and a lake's middle, never a ford, a causeway or a bank's shallow edge.
func TestDeepWaterIsWaterTooDeepToWade(t *testing.T) {
	p := flat()
	p.Features = []Feature{
		{River: &River{Path: [][2]float64{{64, 0}, {64, 128}}, Width: 6, Depth: 2, Fords: [][2]float64{{64, 40}}}},
		{Lake: &Lake{Area: [][2]float64{{90, 90}, {120, 90}, {120, 120}, {90, 120}}, Depth: 1.5}},
		{Lake: &Lake{Area: [][2]float64{{20, 90}, {40, 90}, {40, 110}, {20, 110}}, Depth: 0.8}},
		{Lake: &Lake{Area: [][2]float64{{20, 20}, {40, 20}, {40, 40}, {20, 40}}, Depth: 0.5}},
		{Road: &Road{Path: [][2]float64{{20, 80}, {110, 80}}, Width: 4}},
	}
	r := build(t, p)
	deep := func(x, z int) bool { return r.Geometry.Terrain.Barrier[z*129+x] }
	wet := func(x, z int) bool { return r.Wet[z*129+x] }
	for _, c := range []struct {
		what      string
		x, z      int
		deep, wet bool
	}{
		{"the river's middle", 64, 64, true, true},
		{"2 m from its middle, on the bank's steep fall", 66, 64, true, true},
		{"3 m from its middle, where the bank meets the land", 67, 64, false, true},
		{"at the ford", 64, 40, false, true},
		{"4 m along the river from the ford, its edge", 64, 44, false, true},
		{"on the road across it, a causeway", 64, 80, false, false},
		{"the lake's middle", 105, 105, true, true},
		{"a lake 0.8 m deep, past wading", 30, 100, true, true},
		{"a pond 0.5 m deep, wadeable", 30, 30, false, true},
		{"dry land", 50, 50, false, false},
	} {
		if deep(c.x, c.z) != c.deep || wet(c.x, c.z) != c.wet {
			t.Errorf("%s (%d, %d): deep %v, wet %v; want %v, %v", c.what, c.x, c.z, deep(c.x, c.z), wet(c.x, c.z), c.deep, c.wet)
		}
	}
	if g := r.Geometry.Terrain; !g.Barred(63.5, 63.5) || !g.Barred(66.5, 63.5) || g.Barred(67.5, 63.5) || g.Barred(64.5, 40.5) {
		t.Error("a cell is deep where any of its samples is")
	}
}

// A ford is walked across however tall its banks: no cell is steep in a band 4 m wide across the river through it, while the river's banks away from it stay too steep to climb. Here the land east of the river stands 3 m over the west's, so its bank falls 3.8 m to the water. nav's TestEveryFordIsCrossedBothWays walks every plan's.
func TestAFordIsWalkedAcross(t *testing.T) {
	p := flat()
	p.Features = []Feature{
		{Plateau: &Plateau{Area: [][2]float64{{67, 0}, {128, 0}, {128, 128}, {67, 128}}, Height: 3, Edge: "cliff"}},
		{River: &River{Path: [][2]float64{{64, 0}, {64, 128}}, Width: 6, Depth: 2, Fords: [][2]float64{{64, 40}}}},
	}
	r := build(t, p)
	tr := r.Geometry.Terrain
	steep := func(x, z float64) bool { return tr.Steep(x, z, false) || tr.Steep(x, z, true) }
	for z := 38.5; z < 42; z++ {
		for x := 50.5; x < 78; x++ {
			if steep(x, z) || tr.Barred(x, z) {
				t.Errorf("the cell at (%g, %g), across the ford, is steep or deep", x, z)
			}
		}
	}
	if !steep(66.5, 90.5) {
		t.Error("the east bank at (66.5, 90.5), far from the ford, isn't steep")
	}
	if h := tr.Heights[40*129+64]; math.Abs(h-(r.Rivers[0].Levels[0]-fordDepth)) > 1e-6 {
		t.Errorf("the ford's floor is at %v; want its water's level less %v", h, fordDepth)
	}
}

// A ridge is (1 − d²/r²)² of its height at d from its path, r half its width, and nothing past it or past its ends.
func TestARidgeFallsSmoothlyFromItsPath(t *testing.T) {
	p := flat()
	p.Features = []Feature{{Ridge: &Ridge{Path: [][2]float64{{20, 64}, {108, 64}}, Width: 20, Height: 4}}}
	r := build(t, p)
	near(t, "on the path", r.at(64, 64), 4)
	near(t, "5 m off", r.at(64, 69), 4*0.75*0.75)
	near(t, "10 m off, its edge", r.at(64, 74), 0)
	near(t, "past its end", r.at(8, 64), 0)
}

// A plateau is its height over its area; a cliff edge falls over two samples, too steep to walk, and a ramp at half the walkable slope, walkable.
func TestAPlateausEdgeIsACliffOrARamp(t *testing.T) {
	square := [][2]float64{{40, 40}, {88, 40}, {88, 88}, {40, 88}}
	p := flat()
	p.Features = []Feature{{Plateau: &Plateau{Area: square, Height: 5, Edge: Cliff}}}
	r := build(t, p)
	near(t, "inside", r.at(64, 64), 5)
	near(t, "on its west edge", r.at(40, 64), 5)
	near(t, "1 m out", r.at(39, 64), 2.5)
	near(t, "2 m out", r.at(38, 64), 0)
	near(t, "on its east edge", r.at(88, 64), 5)
	if !r.steepBetween(38, 64, false) || !r.steepBetween(39, 64, false) {
		t.Error("a cliff's two cells aren't too steep to walk")
	}

	p.Features[0].Plateau.Edge, p.Features[0].Plateau.Height = Ramp, 3
	r = build(t, p)
	run := 3 / (body(t).MaxSlope / 2)
	near(t, "a ramp 1 m out", r.at(39, 64), 3*(1-1/run))
	for x := 28; x < 40; x++ {
		if r.steepBetween(x, 64, false) {
			t.Errorf("the ramp's cell at x %d is too steep to walk", x)
		}
	}
	if r.at(int(40-run)-1, 64) != 0 {
		t.Errorf("height %v past the ramp's foot; want 0", r.at(int(40-run)-1, 64))
	}
}

// A road through a ford wades it: the ford's floor stays fordDepth under the water, wet and not deep across the river's width, and the crossing walkable, where the road alone would make a dry causeway with banks falling square to the water; the same road over the river away from a ford is that causeway.
func TestARoadThroughAFordWadesIt(t *testing.T) {
	p := flat()
	p.Features = []Feature{
		{River: &River{Path: [][2]float64{{64, 0}, {64, 128}}, Width: 6, Depth: 2, Fords: [][2]float64{{64, 40}}}},
		{Road: &Road{Path: [][2]float64{{20, 40}, {110, 40}}, Width: 4}},
		{Road: &Road{Path: [][2]float64{{20, 90}, {110, 90}}, Width: 4}},
	}
	r := build(t, p)
	tr := r.Geometry.Terrain
	level := r.Rivers[0].Levels[0]
	if h := tr.Heights[40*129+64]; math.Abs(h-(level-fordDepth)) > 1e-6 {
		t.Errorf("the ford's floor under the road is at %v; want its water's level %v less %v", h, level, fordDepth)
	}
	for x := 62; x <= 66; x++ {
		if s := 40*129 + x; !r.Wet[s] || tr.Barrier[s] || tr.Heights[s] >= level {
			t.Errorf("(%d, 40), in the ford on the road: wet %v, deep %v, ground %v under water at %v", x, r.Wet[s], tr.Barrier[s], tr.Heights[s], level)
		}
	}
	for z := 38.5; z < 42; z++ {
		for x := 50.5; x < 78; x++ {
			if tr.Steep(x, z, false) || tr.Steep(x, z, true) || tr.Barred(x, z) {
				t.Errorf("the cell at (%g, %g), on the road through the ford, is steep or deep", x, z)
			}
		}
	}
	if s := 90*129 + 64; r.Wet[s] || tr.Heights[s] < level {
		t.Errorf("the road over the river at (64, 90), no ford: wet %v, ground %v; want a dry causeway over the water at %v", r.Wet[s], tr.Heights[s], level)
	}
}

// A road that only passes a ford, along its bank inside the reach of the ford's ramp, bakes as it did before roads waded fords: its heights hash as main's generator made them.
func TestARoadPassingAFordIsUnchanged(t *testing.T) {
	p := flat()
	p.Features = []Feature{
		{River: &River{Path: [][2]float64{{64, 0}, {64, 128}}, Width: 6, Depth: 2, Fords: [][2]float64{{64, 40}}}},
		{Road: &Road{Path: [][2]float64{{71, 4}, {71, 124}}, Width: 4}},
	}
	h := sha256.New()
	for _, v := range build(t, p).Geometry.Terrain.Heights {
		_ = binary.Write(h, binary.LittleEndian, v)
	}
	const want = "84428b8798d7c405f20f2bd260611c2df3bfa9a119537bda7094569d9d41e0a0"
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		t.Errorf("heights hash %s; want %s, as before", got, want)
	}
}

// A causeway away from any ford stays dry, as it always was, even where a wide river's channel pulls the road's mean under the water: only in a ford the road crosses is it wet.
func TestACausewayStaysDryUnderItsMean(t *testing.T) {
	p := flat()
	p.Features = []Feature{
		{River: &River{Path: [][2]float64{{64, 0}, {64, 128}}, Width: 20, Depth: 3}},
		{Road: &Road{Path: [][2]float64{{4, 64}, {124, 64}}, Width: 4}},
	}
	r := build(t, p)
	s := 64*129 + 64
	if h, level := r.Geometry.Terrain.Heights[s], r.Rivers[0].Levels[0]; h >= level {
		t.Fatalf("the causeway's ground at %v stands over the water at %v; the case needs it under", h, level)
	}
	if r.Wet[s] || r.Geometry.Terrain.Barrier[s] {
		t.Errorf("the causeway at (64, 64): wet %v, deep %v; want dry", r.Wet[s], r.Geometry.Terrain.Barrier[s])
	}
}

// No sample under a road is a barrier, in the test plans or the shipped regions: a causeway is dry, and a ford a road wades is shallow.
func TestNoRoadIsABarrier(t *testing.T) {
	for name, p := range everyPlan(t) {
		r := build(t, p)
		nx, nz := samples(p.Size[0], p.Cell), samples(p.Size[1], p.Cell)
		for _, f := range p.Features {
			if f.Road == nil {
				continue
			}
			path := smooth(f.Road.Path)
			for s := range nx * nz {
				at := [2]float64{float64(s%nx) * p.Cell, float64(s/nx) * p.Cell}
				if r.Geometry.Terrain.Barrier[s] && onPath(path, at, float64(f.Road.Width/2)) {
					t.Errorf("%s: the road through %v is a barrier at %v", name, f.Road.Path[0], at)
				}
			}
		}
	}
}

// everyPlan is the test plans with rivers and roads, and the shipped regions, by name.
func everyPlan(t *testing.T) map[string]Plan {
	plans := map[string]Plan{"small": small(t), "wide": plan(t, "testdata/wide.yaml")}
	shipped, err := filepath.Glob("../../../data/regions/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range shipped {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if plans[path], err = Decode(raw); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	return plans
}

// Every ford of the test plans and the shipped regions holds water over its middle, fordDepth deep, wet and not deep, whatever crosses it.
func TestEveryFordHoldsShallowWater(t *testing.T) {
	for name, p := range everyPlan(t) {
		r := build(t, p)
		tr := r.Geometry.Terrain
		nx := samples(p.Size[0], p.Cell)
		rivers := 0
		for _, f := range p.Features {
			if f.River == nil {
				continue
			}
			water := r.Rivers[rivers]
			rivers++
			for _, at := range f.River.Fords {
				d2 := func(q [2]float64) float64 {
					return float64((q[0]-at[0])*(q[0]-at[0])) + float64((q[1]-at[1])*(q[1]-at[1]))
				}
				near := 0 // the river's point nearest the ford, whose level is the water over it
				for k, q := range water.Path {
					if d2(q) < d2(water.Path[near]) {
						near = k
					}
				}
				x, z := int(math.Round(at[0]/p.Cell)), int(math.Round(at[1]/p.Cell))
				s := z*nx + x
				if depth := water.Levels[near] - tr.Heights[s]; !r.Wet[s] || tr.Barrier[s] || depth <= 0 || depth > wadeDepth {
					t.Errorf("%s: the ford at %v: wet %v, deep %v, water %v over it", name, at, r.Wet[s], tr.Barrier[s], depth)
				}
			}
		}
	}
}

// A river's bed lies its depth under water half a metre below the ground, between banks too steep to climb, wet across its width; at a ford it's shallow, and its banks walkable.
func TestARiverCarvesABedWithSteepBanksAndAWalkableFord(t *testing.T) {
	p := flat()
	p.Features = []Feature{{River: &River{Path: [][2]float64{{0, 64}, {128, 60}}, Width: 10, Depth: 2, Fords: [][2]float64{{64, 62}}}}}
	r := build(t, p)
	water := -freeboard
	near(t, "its middle", r.at(32, 63), water-2)
	near(t, "its middle at the ford", r.at(64, 62), water-fordDepth)
	steep := false
	for z := 55; z < 70; z++ {
		steep = steep || r.steepBetween(32, z, true)
		if r.steepBetween(64, z, true) {
			t.Errorf("the ford's crossing has a steep cell at z %d", z)
		}
	}
	if !steep {
		t.Error("no steep bank across the river away from the ford")
	}
	if !r.Wet[63*129+32] || r.Wet[70*129+32] {
		t.Error("wet marks: want the river's middle wet and the ground 7 m off dry")
	}
	lv := r.Rivers[0].Levels
	for i := 1; i < len(lv); i++ {
		if lv[i] > lv[i-1] {
			t.Fatalf("level %d rises downstream: %v after %v", i, lv[i], lv[i-1])
		}
	}
}

// A river's water follows the ground down and never climbs: drawn up a slope, it stays at the lowest it has been.
func TestARiversWaterFallsWithTheGroundAndNeverRises(t *testing.T) {
	p := flat()
	p.Features = []Feature{
		{Plateau: &Plateau{Area: [][2]float64{{0, 0}, {64, 0}, {64, 128}, {0, 128}}, Height: 2, Edge: Ramp}},
		{River: &River{Path: [][2]float64{{0, 64}, {128, 64}}, Width: 6, Depth: 1}},
	}
	r := build(t, p)
	lv := r.Rivers[0].Levels
	if first, last := lv[0], lv[len(lv)-1]; first != f32(2-freeboard) || last != f32(-freeboard) {
		t.Errorf("levels from %v to %v; want from %v on the plateau to %v below it", first, last, 2-freeboard, -freeboard)
	}

	p.Features[1].River.Path = [][2]float64{{128, 64}, {0, 64}} // drawn uphill
	r = build(t, p)
	for _, l := range r.Rivers[0].Levels {
		if l != f32(-freeboard) {
			t.Fatalf("a level of %v; want every level %v, the lowest at its start", l, -freeboard)
		}
	}
}

// A lake's bed lies its depth under water half a metre below the lowest ground along its edge, its banks steep.
func TestALakeIsCarvedBelowItsLowestShore(t *testing.T) {
	p := flat()
	p.Features = []Feature{{Lake: &Lake{Area: [][2]float64{{80, 20}, {110, 20}, {110, 50}, {80, 50}}, Depth: 3}}}
	r := build(t, p)
	near(t, "its middle", r.at(95, 35), -freeboard-3)
	near(t, "on its edge", r.at(80, 35), 0)
	near(t, "1 m in", r.at(81, 35), (-freeboard-3)/2)
	near(t, "its water", r.Lakes[0].Level, -freeboard)
	if !r.steepBetween(80, 35, false) {
		t.Error("the lake's bank isn't too steep to climb")
	}
}

// A road is level across its width, walkable along it, and dirt; its shoulder eases back to the ground beside it.
func TestARoadIsLevelAcrossAndWalkableAlong(t *testing.T) {
	p := flat()
	p.Ground.Noise = Noise{Scale: 40, Amplitude: 4, Octaves: 3}
	p.Features = []Feature{{Road: &Road{Path: [][2]float64{{10, 64}, {118, 64}}, Width: 6}}}
	r := build(t, p)
	dirt := slicesIndex(r.Palette, roadColour)
	for x := 20; x <= 100; x++ {
		for z := 61; z <= 67; z++ {
			if r.at(x, z) != r.at(x, 64) {
				t.Fatalf("at x %d the road is %v at z %d and %v at its middle; want it level across", x, r.at(x, z), z, r.at(x, 64))
			}
			if int(r.Colours[z*129+x]) != dirt {
				t.Fatalf("(%d, %d) on the road isn't dirt", x, z)
			}
		}
		if r.steepBetween(x, 64, false) {
			t.Errorf("the road is too steep to walk at x %d", x)
		}
	}
}

// A town is flat at the mean height of the ground it was, easing back to the land around.
func TestATownIsFlattenedToItsMean(t *testing.T) {
	p := flat()
	p.Ground.Noise = Noise{Scale: 40, Amplitude: 4, Octaves: 3}
	town := [][2]float64{{40, 40}, {80, 40}, {80, 80}, {40, 80}}
	p.Areas = append([]Area{{ID: "town", Area: town, Biome: "meadow", Kind: Hunting}}, p.Areas...)
	before := build(t, p)
	sum, n := 0.0, 0
	for z := 40; z < 80; z++ {
		for x := 40; x < 80; x++ {
			sum, n = sum+before.at(x, z), n+1
		}
	}
	p.Areas[0].Kind = Town
	r := build(t, p)
	for z := 40; z < 80; z++ {
		for x := 40; x < 80; x++ {
			if r.at(x, z) != r.at(40, 40) {
				t.Fatalf("(%d, %d) is at %v and the town's corner at %v; want it flat", x, z, r.at(x, z), r.at(40, 40))
			}
		}
	}
	if mean := sum / float64(n); math.Abs(r.at(60, 60)-mean) > 1e-4 {
		t.Errorf("the town at %v; want the mean of the ground it was, %v", r.at(60, 60), mean)
	}
	if r.at(100, 60) != before.at(100, 60) {
		t.Errorf("ground 20 m from the town moved from %v to %v", before.at(100, 60), r.at(100, 60))
	}
}

// Each sample takes its biome's colour by the rules in order: road, wet, too steep to walk, above the high mark, sloped past the plan's slope, else flat; the edge's biome outside every area.
func TestColoursFollowTheGroundsShape(t *testing.T) {
	p := flat()
	p.Colour.High = 4
	p.Areas[0].Area = [][2]float64{{0, 0}, {100, 0}, {100, 128}, {0, 128}}
	p.Biomes["peaks"] = Biome{Colours: Colours{Flat: "brick", Slope: "brick", Steep: "brick", High: "brick"}}
	p.Edge.Biome = "peaks"
	p.Features = []Feature{
		{Plateau: &Plateau{Area: [][2]float64{{20, 20}, {40, 20}, {40, 40}, {20, 40}}, Height: 5, Edge: Cliff}},
		{Plateau: &Plateau{Area: [][2]float64{{60, 20}, {80, 20}, {80, 40}, {60, 40}}, Height: 2, Edge: Ramp}},
	}
	r := build(t, p)
	for _, c := range []struct {
		x, z int
		want string
	}{
		{10, 80, "grass"}, // flat, low
		{30, 30, "snow"},  // the cliff plateau's top, above the high mark
		{19, 30, "stone"}, // its cliff
		{57, 30, "dirt"},  // the ramp, past the colour slope
		{110, 80, "brick"},
	} {
		if got := r.Palette[r.Colours[c.z*129+c.x]]; got != c.want {
			t.Errorf("(%d, %d) is %s; want %s", c.x, c.z, got, c.want)
		}
	}
}

// Every number the bake stores is a float32's, so the server, which keeps them, reads the ground the client does.
func TestEveryStoredNumberIsAFloat32s(t *testing.T) {
	r := smallRegion(t)
	exact := func(what string, v float64) {
		if v != f32(v) {
			t.Fatalf("%s %v isn't a float32's", what, v)
		}
	}
	for _, h := range r.Geometry.Terrain.Heights {
		exact("a height", h)
	}
	for _, c := range r.Geometry.Cylinders {
		for _, v := range [...]float64{c.X, c.Z, c.Radius, c.Top} {
			exact("a cylinder's", v)
		}
	}
	for _, b := range r.Geometry.Boxes {
		for _, v := range [...]float64{b.X, b.Z, b.HalfW, b.HalfD, b.UX, b.UZ, b.Top} {
			exact("a box's", v)
		}
	}
	for _, p := range r.Props {
		for _, v := range [...]float64{p.X, p.Z, p.Yaw} {
			exact("a prop's", v)
		}
	}
	for _, rv := range r.Rivers {
		exact("a river's width", rv.Width)
		for _, v := range rv.Levels {
			exact("a river level", v)
		}
		for _, at := range rv.Path {
			exact("a river's path", at[0])
			exact("a river's path", at[1])
		}
	}
	for _, l := range r.Lakes {
		exact("a lake's level", l.Level)
		for _, at := range l.Area {
			exact("a lake's corner", at[0])
			exact("a lake's corner", at[1])
		}
	}
}

// Each waypoint stands its stone, a solid of 0.5 m to the obelisk's top (large-regions.md section 8), with no prop: the client draws the stone, lit or dim.
func TestEachWaypointStandsItsStone(t *testing.T) {
	r := smallRegion(t)
	if len(r.Plan.Waypoints) == 0 {
		t.Fatal("the small plan has no waypoint to stand a stone at")
	}
	for _, w := range r.Plan.Waypoints {
		x, z := f32(w.At[0]), f32(w.At[1])
		k := slices.IndexFunc(r.Geometry.Cylinders, func(c sim.Cylinder) bool { return c.X == x && c.Z == z })
		if k < 0 {
			t.Errorf("waypoint %s at %v stands no solid", w.ID, w.At)
			continue
		}
		c := r.Geometry.Cylinders[k]
		if want := f32(r.Geometry.Terrain.Height(x, z) + 2.82); c.Radius != 0.5 || c.Top != want { // the model's obelisk top, web/scripts/waypoint_stone.py
			t.Errorf("waypoint %s's stone %+v; want radius 0.5 to %v", w.ID, c, want)
		}
		if slices.ContainsFunc(r.Props, func(p Prop) bool { return p.X == x && p.Z == z }) {
			t.Errorf("waypoint %s has a prop drawn by the bake", w.ID)
		}
	}
}

// smallRegion is small built once and shared: tests only read it.
func smallRegion(t testing.TB) *Region {
	t.Helper()
	builtSmall.once.Do(func() { builtSmall.r, builtSmall.err = Generate(small(t), shippedKit(t), body(t)) })
	if builtSmall.err != nil {
		t.Fatal(builtSmall.err)
	}
	return builtSmall.r
}

var builtSmall struct {
	once sync.Once
	r    *Region
	err  error
}

// smallBake is smallRegion's bake, made once.
func smallBake(t testing.TB) Bake {
	t.Helper()
	r := smallRegion(t)
	bakedSmall.once.Do(func() { bakedSmall.b, bakedSmall.err = r.Bake() })
	if bakedSmall.err != nil {
		t.Fatal(bakedSmall.err)
	}
	return bakedSmall.b
}

var bakedSmall struct {
	once sync.Once
	b    Bake
	err  error
}

// small is testdata/small.yaml: one of everything the generator builds.
func small(t testing.TB) Plan {
	t.Helper()
	return plan(t, "testdata/small.yaml")
}

func plan(t testing.TB, path string) Plan {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func slicesIndex(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// A road across a river is a causeway: dry, and level across like the rest of it.
func TestARoadOverARiverIsACauseway(t *testing.T) {
	p := flat()
	p.Features = []Feature{
		{River: &River{Path: [][2]float64{{0, 64}, {128, 64}}, Width: 10, Depth: 2}},
		{Road: &Road{Path: [][2]float64{{64, 10}, {64, 118}}, Width: 4}},
	}
	r := build(t, p)
	if r.Wet[64*129+64] || !r.Wet[64*129+40] {
		t.Error("want the road across the river dry and the river beside it wet")
	}
	if r.at(63, 64) != r.at(64, 64) || r.at(64, 64) < -freeboard {
		t.Errorf("the causeway at %v and %v; want it level and above the water", r.at(63, 64), r.at(64, 64))
	}
}

// Where two areas overlap, the first holds the ground: its biome colours it.
func TestTheFirstOfOverlappingAreasHoldsTheGround(t *testing.T) {
	p := flat()
	p.Biomes["snowfield"] = Biome{Colours: Colours{Flat: "snow", Slope: "snow", Steep: "snow", High: "snow"}}
	p.Areas = append([]Area{{ID: "snow", Area: [][2]float64{{0, 0}, {64, 0}, {64, 128}, {0, 128}}, Biome: "snowfield", Kind: Hunting}}, p.Areas...)
	r := build(t, p)
	if got := r.Palette[r.Colours[64*129+32]]; got != "snow" {
		t.Errorf("ground in both areas is %s; want the first's snow", got)
	}
	if got := r.Palette[r.Colours[64*129+96]]; got != "grass" {
		t.Errorf("ground in the second alone is %s; want its grass", got)
	}
}
