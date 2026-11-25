package regiongen

import (
	"math"
	"slices"
	"testing"

	"finga/internal/shared/mapfile"
	"finga/internal/shared/sim"
)

// torchesOf are r's props of model, in the order hung.
func torchesOf(r *Region, model string) []Prop {
	m := slices.Index(r.Models, model)
	return slices.DeleteFunc(slices.Clone(r.Props), func(p Prop) bool { return p.Model != m })
}

// A tunnel's torches hang one every spacing along its left wall, then its right, staggered half a spacing, each on the floor by the wall's foot facing across; torches next to each other on a wall flicker apart.
func TestTorchesHangAlongATunnelsTwoWalls(t *testing.T) {
	p := flat()
	cave(&p)
	p.Features = []Feature{{Tunnel: &Tunnel{Path: [][2]float64{{16, 64}, {112, 64}}, Width: 6}}}
	p.Torches = &Torches{Spacing: 10, Model: "torch_mounted"}
	r := build(t, p)
	hung := torchesOf(r, "torch_mounted")
	if len(hung) != 20 || len(r.Flames) != 20 {
		t.Fatalf("%d torches, %d flames; want ten on each wall of the 96 m tunnel", len(hung), len(r.Flames))
	}
	for k, h := range hung {
		south := k >= 10 // left of an eastward path is north
		if off := math.Abs(h.Z - 64); off < 1.5 || off > 2.5 || (h.Z < 64) != south {
			t.Fatalf("torch %d at (%v, %v); want by the %s wall's foot, a cell and a half short of the rock 3 m off the path", k, h.X, h.Z, map[bool]string{true: "south", false: "north"}[south])
		}
		if want := map[bool]float64{true: 0, false: 180}[south]; h.Yaw != want {
			t.Errorf("torch %d faces %v; want %v, across the tunnel", k, h.Yaw, want)
		}
		if want := 16 + float64(10*(k%10)) + map[bool]float64{true: 5}[south]; math.Abs(h.X-want) > 0.5 {
			t.Errorf("torch %d at x = %v; want %v, one every 10 m, the south wall's 5 m on", k, h.X, want)
		}
		if k%10 > 0 && r.Flames[k].Group == r.Flames[k-1].Group {
			t.Errorf("torches %d and %d, next to each other on a wall, flicker together", k-1, k)
		}
	}
}

// A hall's torches hang round its walls facing in, its corners listed either way round, kept torchAway from its gate, waypoint, other torches and a hand-placed one, and out of its camp's and boss's circles; where a tunnel opens there's no wall to hang on.
func TestAHallsTorchesKeepClear(t *testing.T) {
	hall := [][2]float64{{24, 24}, {104, 24}, {104, 104}, {24, 104}}
	t.Run("anticlockwise", func(t *testing.T) { hallTorchesKeepClear(t, hall) })
	t.Run("clockwise", func(t *testing.T) {
		cw := slices.Clone(hall)
		slices.Reverse(cw)
		hallTorchesKeepClear(t, cw)
	})
}

func hallTorchesKeepClear(t *testing.T, hall [][2]float64) {
	p := flat()
	cave(&p)
	p.Features = []Feature{
		{Hall: &Hall{Area: hall}},
		{Tunnel: &Tunnel{Path: [][2]float64{{64, 100}, {64, 118}}, Width: 6}},
	}
	p.Torches = &Torches{Spacing: 8, Model: "torch_mounted"}
	p.Gates = []Gate{{Name: "up", At: [2]float64{26, 68}, Radius: 1.6}}
	p.Waypoints = []Waypoint{{ID: "w", At: [2]float64{44, 102}}}
	p.Camps = []Camp{{Area: "all", Name: "bats", At: [2]float64{100, 64}, Radius: 6}}
	p.Areas[0].Boss = &Boss{At: [2]float64{60, 27}, Radius: 5}
	p.Pieces = []Piece{{Model: "torch_lit", At: &[2]float64{84, 26}}}
	r := build(t, p)
	hung := torchesOf(r, "torch_mounted")
	if len(hung) < 30 {
		t.Fatalf("%d torches round a 320 m wall at 8 m; want most of 40", len(hung))
	}
	away := func(h Prop, at [2]float64, r float64) bool {
		dx, dz := h.X-at[0], h.Z-at[1]
		return float64(dx*dx)+float64(dz*dz) > float64(r*r)
	}
	for _, h := range append(hung, torchesOf(r, "torch_lit")...) {
		for _, o := range append(hung, torchesOf(r, "torch_lit")...) {
			if o != h && !away(h, [2]float64{o.X, o.Z}, torchAway) {
				t.Fatalf("torches at (%v, %v) and (%v, %v), closer than %d m", h.X, h.Z, o.X, o.Z, torchAway)
			}
		}
	}
	for _, h := range hung {
		switch {
		case !away(h, p.Gates[0].At, torchAway), !away(h, p.Waypoints[0].At, torchAway):
			t.Errorf("a torch at (%v, %v), within %d m of the gate or the waypoint", h.X, h.Z, torchAway)
		case !away(h, p.Camps[0].At, p.Camps[0].Radius), !away(h, p.Areas[0].Boss.At, p.Areas[0].Boss.Radius):
			t.Errorf("a torch at (%v, %v), in the camp's or the boss's circle", h.X, h.Z)
		case h.X > 60 && h.X < 68 && h.Z > 100 && h.Z < 106:
			t.Errorf("a torch at (%v, %v), in the tunnel's mouth", h.X, h.Z)
		}
		if h.Z > 106 {
			continue // the tunnel's own, across it
		}
		sides := []float64{h.X - 24, 104 - h.X, h.Z - 24, 104 - h.Z} // the nearest wall: west, east, south, north
		want := []float64{90, 270, 0, 180}[slices.Index(sides, slices.Min(sides))]
		if h.Yaw != want {
			t.Errorf("a torch at (%v, %v) faces %v; want %v, into the hall", h.X, h.Z, h.Yaw, want)
		}
	}
}

// Where the wall wanders, a hall's torches still stand by its foot: each on carved floor, the rock a cell and a half behind it.
func TestTorchesHangAtAWanderingWallsFoot(t *testing.T) {
	p := flat()
	cave(&p)
	p.Rock.Rough = 1.5
	p.Features = []Feature{{Hall: &Hall{Area: [][2]float64{{24, 24}, {104, 24}, {104, 104}, {24, 104}}}}}
	p.Torches = &Torches{Spacing: 8, Model: "torch_mounted"}
	r := build(t, p)
	hung := torchesOf(r, "torch_mounted")
	if len(hung) < 36 {
		t.Fatalf("%d torches round a wandering 320 m wall at 8 m; want nearly 40", len(hung))
	}
	cell := r.Geometry.Terrain.Cell
	rock := func(x, z float64) bool {
		return r.colourAt(int(math.Floor(x/cell+0.5)), int(math.Floor(z/cell+0.5))) == rockColour
	}
	for _, h := range hung {
		ux, uz := mapfile.Axis(h.Yaw)
		if back := float64(1.5 * cell); rock(h.X, h.Z) || !rock(h.X+float64(uz*back), h.Z-float64(ux*back)) {
			t.Errorf("a torch at (%v, %v) facing %v; want it on carved floor, the rock a cell and a half behind", h.X, h.Z, h.Yaw)
		}
	}
}

// Every torch the test cave hangs stands on the floor by a wall's foot, facing into the carved space: floor a metre ahead, the wall risen 2 m behind.
func TestTheTestCavesTorchesStandAtTheirWallsFeet(t *testing.T) {
	p := plan(t, "testdata/cave.yaml")
	r := build(t, p)
	hung := torchesOf(r, p.Torches.Model)
	if len(hung) < 10 {
		t.Fatalf("%d torches", len(hung))
	}
	tr := r.Geometry.Terrain
	for _, h := range hung {
		f := facings[int(math.Round(h.Yaw/22.5))%len(facings)]
		at := tr.Height(h.X, h.Z)
		ahead, behind := tr.Height(h.X+f[0], h.Z+f[1]), tr.Height(h.X-float64(2*f[0]), h.Z-float64(2*f[1]))
		if math.Abs(ahead-at) > 0.5 || behind-at < p.Rock.Height/2 {
			t.Errorf("the torch at (%g, %g) facing %v: the ground %g ahead and %g behind; want floor ahead, the wall behind", h.X, h.Z, f, ahead-at, behind-at)
		}
	}
}

// Torches of one group close together light the ground between them fully, never past it.
func TestTheLightMapClampsAtFull(t *testing.T) {
	k := shippedKit(t)
	k.Models["test_torch"] = Solid{Light: &Light{Height: 2, Reach: 12}}
	p := flat()
	cave(&p)
	p.Features = []Feature{{Hall: &Hall{Area: [][2]float64{{32, 32}, {96, 32}, {96, 96}, {32, 96}}}}}
	p.Pieces = []Piece{{Model: "test_torch", At: &[2]float64{60, 64}}, {Model: "test_torch", At: &[2]float64{40, 40}}, {Model: "test_torch", At: &[2]float64{61, 64}}}
	r, err := Generate(p, k, body(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := r.lightMap()[lightGroups*(64*128+60)]; got != 255 {
		t.Errorf("between the two group 0 torches: %d; want 255", got)
	}
}

// The light map holds each flicker group's light: (1 − d²/r²)² of a torch where it sees the ground, none behind a wall it can't see past or on rock above its flame; neighbouring torches fall in turn into the two groups.
func TestTheLightMapFallsOffAndStopsAtWalls(t *testing.T) {
	k := shippedKit(t)
	k.Models["test_torch"] = Solid{Light: &Light{Height: 2, Reach: 12}}
	p := flat()
	cave(&p)
	p.Features = []Feature{ // two halls a rib of rock apart, its top from x = 62 to 63
		{Hall: &Hall{Area: [][2]float64{{32, 32}, {60, 32}, {60, 96}, {32, 96}}}},
		{Hall: &Hall{Area: [][2]float64{{65, 32}, {96, 32}, {96, 96}, {65, 96}}}},
	}
	p.Pieces = []Piece{{Model: "test_torch", At: &[2]float64{59, 64}}, {Model: "test_torch", At: &[2]float64{40, 40}}}
	r, err := Generate(p, k, body(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Flames) != 2 || r.Flames[0].Group != 0 || r.Flames[1].Group != 1 || r.Flames[0].Y != 2 {
		t.Fatalf("flames %+v; want the two torches', 2 m up, in groups 0 and 1", r.Flames)
	}
	light := r.lightMap()
	at := func(x, z, group int) byte { return light[lightGroups*(z*128+x)+group] }
	falloff := func(x, z float64) byte {
		d2 := float64((x-59)*(x-59)) + float64((z-64)*(z-64))
		w := 1 - d2/144
		return byte(math.Floor(float64(w*w) * 255))
	}
	for _, c := range []struct {
		what string
		x, z int
		want byte
	}{
		{"by the torch", 58, 64, falloff(58.5, 64.5)},
		{"4.5 m off in its hall", 54, 64, falloff(54.5, 64.5)},
		{"9.5 m off in its hall", 49, 64, falloff(49.5, 64.5)},
		{"past its reach", 45, 64, 0},
		{"past its reach, toward a corner of the square it covers", 48, 55, 0},
		{"on the rib's rock, above the flame", 62, 64, 0},
		{"in the next hall, behind the rib", 66, 64, 0},
	} {
		if got := at(c.x, c.z, 0); got != c.want {
			t.Errorf("%s: group 0's light %d; want %d", c.what, got, c.want)
		}
	}
	if at(54, 64, 0) == 0 || at(54, 64, 1) != 0 || at(40, 40, 1) == 0 || at(40, 40, 0) != 0 {
		t.Error("each torch's light isn't in its own group")
	}
}

// Ground above a flame stays dark even where the torch sees it: up a gentle ramp, nothing rising between, the light stops where the ground passes the flame.
func TestGroundAboveTheFlameStaysDark(t *testing.T) {
	heights := make([]float64, 17*3)
	for i := range heights {
		heights[i] = float64(i%17) / 2 // 0.5 m a metre up toward the east
	}
	r := &Region{Plan: Plan{Size: [2]float64{16, 2}}, Flames: []Flame{{At: [2]float64{1, 1}, Y: 3, Reach: 12}}}
	r.Geometry.Terrain = sim.NewTerrain(1, 17, 3, heights, 1)
	light := r.lightMap()
	for x := 2; x < 13; x++ {
		ground := (float64(x) + 0.5) / 2
		if lit := light[lightGroups*(16+x)] > 0; lit != (ground < 3) {
			t.Errorf("texel x = %d, its ground %v m up under a flame 3 m up, lit %v", x, ground, lit)
		}
	}
}

// Scatter keeps its away distance from a torch, which stands no solid to keep it off; distances are judged at the nearest sample.
func TestScatterKeepsClearOfTorches(t *testing.T) {
	p := flat()
	cave(&p)
	p.Features = []Feature{{Hall: &Hall{Area: [][2]float64{{24, 24}, {104, 24}, {104, 104}, {24, 104}}}}}
	p.Torches = &Torches{Spacing: 8, Model: "torch_mounted"}
	p.Biomes["meadow"] = Biome{Colours: flat().Biomes["meadow"].Colours, Scatter: []Scatter{{Model: "pebbles", Spacing: 1, Away: Away{Torch: 1.5}}}}
	r := build(t, p)
	torches, pebbles := torchesOf(r, "torch_mounted"), torchesOf(r, "pebbles")
	if len(torches) < 30 || len(pebbles) < 1000 {
		t.Fatalf("%d torches, %d pebbles", len(torches), len(pebbles))
	}
	slack := float64(math.Sqrt2 / 2 * p.Cell)
	near := 0
	for _, tc := range torches {
		for _, pb := range pebbles {
			dx, dz := pb.X-tc.X, pb.Z-tc.Z
			d := math.Sqrt(float64(dx*dx) + float64(dz*dz))
			if d <= 1.5-slack {
				t.Fatalf("a pebble at (%g, %g), %g m from the torch at (%g, %g); want past 1.5 m", pb.X, pb.Z, d, tc.X, tc.Z)
			}
			if d < 3 {
				near++
			}
		}
	}
	if near == 0 {
		t.Fatal("no pebble within 3 m of a torch: the floor by the walls bare")
	}
}
