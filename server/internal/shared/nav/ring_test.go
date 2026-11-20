package nav_test

import (
	"fmt"
	"math"
	"os"
	"slices"
	"testing"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
	"finga/internal/shared/regiongen"
	"finga/internal/shared/sim"
)

// ringDepth is how far (x, z) lies inside plan's ring of mountains: past 0, in it.
func ringDepth(plan regiongen.Plan, x, z float64) float64 {
	return plan.Edge.Width - min(x, z, plan.Size[0]-x, plan.Size[1]-z)
}

// No player walks or jumps onto a region's ring of mountains (large-regions.md section 3) or into its deep water (decision 9): nothing the walk grid reaches from the start lies in either.
func TestNoPlayerReachesARegionsRingOrDeepWater(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := shipped(t).params
	for id, plan := range shippedRegions(t) {
		t.Run(id, func(t *testing.T) {
			m, _ := data.Map(id)
			got := m.Grid().Reach([]nav.Point{mapfile.Point(plan.Start.At)}, p.Radius, fastRise(data, p))
			for z := 0.25; z < plan.Size[1]; z += 0.5 {
				for x := 0.25; x < plan.Size[0]; x += 0.5 {
					if !got.Has(nav.Point{X: x, Z: z}) {
						continue
					}
					if ringDepth(plan, x, z) > 0 {
						t.Fatalf("a player reaches (%g, %g), %g m into the ring", x, z, ringDepth(plan, x, z))
					}
					if m.Geometry.Terrain.Deep(x, z) {
						t.Fatalf("a player reaches (%g, %g), in deep water", x, z)
					}
				}
			}
		})
	}
}

// Nor does any skill's move (large-regions.md section 3). On each side of every shipped region, and of a flat one whose ground inside is all as high as at its face, a body stands at the face where the ground inside is highest, on it or on the highest kit solid it climbs to. It jumps or not, and fires each move at the ring with its key held 2 s: the body's centre gets no farther into the ring than the cliff's own cell, whose face rises past it.
func TestNoMoveGetsOntoARegionsRing(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := data.Movement()
	type fired struct {
		what string
		move gamedata.Move
	}
	moves := []fired{{what: "no skill"}}
	for _, s := range data.Skills() {
		stages := slices.Clone(s.Stages)
		for _, tr := range s.Traits {
			stages = append(stages, tr.Stage)
		}
		for _, st := range stages {
			if st.Move.Kind != "" {
				moves = append(moves, fired{fmt.Sprintf("%s's %s", s.ID, st.Name), st.Move})
			}
		}
	}
	if len(moves) < 4 {
		t.Fatalf("only %d moves", len(moves)-1)
	}
	kit := shippedKit(t)
	stand := climbed(kit, nav.JumpRise(p))
	if stand < 4 {
		t.Fatalf("the highest a player climbs on kit solids: %g m", stand)
	}
	type region struct {
		plan regiongen.Plan
		g    sim.Geometry
	}
	regions := map[string]region{}
	for id, plan := range shippedRegions(t) {
		m, _ := data.Map(id)
		regions[id] = region{plan, m.Geometry}
	}
	flat := flatRegion()
	r, err := regiongen.Generate(flat, kit, p.MaxSlope)
	if err != nil {
		t.Fatal(err)
	}
	regions["flat"] = region{flat, r.Geometry}
	sides := []struct {
		name  string
		x, z  float64 // the way to the ring
		press sim.Input
	}{{"west", -1, 0, sim.Input{Left: true}}, {"east", 1, 0, sim.Input{Right: true}}, {"south", 0, -1, sim.Input{Down: true}}, {"north", 0, 1, sim.Input{Up: true}}}
	for id, rg := range regions {
		plan, g := rg.plan, rg.g
		w, size := plan.Edge.Width, plan.Size
		for _, side := range sides {
			t.Run(id+" "+side.name, func(t *testing.T) {
				best, found := nav.Point{}, false // the face's samples on this side, the ring's corners aside
				for a := w; a <= size[0]-w; a += plan.Cell {
					at := nav.Point{X: a, Z: w}
					switch side.name {
					case "west":
						at = nav.Point{X: w, Z: a}
					case "east":
						at = nav.Point{X: size[0] - w, Z: a}
					case "north":
						at = nav.Point{X: a, Z: size[1] - w}
					}
					if !nav.FitsAt(g, p.Step, at, p.Radius) {
						continue
					}
					if !found || g.Terrain.Height(at.X, at.Z) > g.Terrain.Height(best.X, best.Z) {
						best, found = at, true
					}
				}
				if !found {
					t.Fatal("no spot at the face a player fits")
				}
				for _, mv := range moves {
					for _, up := range []float64{0, stand} {
						for _, peak := range []bool{false, true} {
							s := sim.State{X: best.X, Y: g.Terrain.Height(best.X, best.Z) + up, Z: best.Z, OnGround: true, HX: side.x, HZ: side.z}
							check := func(when string) {
								if d := ringDepth(plan, s.X, s.Z); d > plan.Cell {
									t.Fatalf("%s from %g m up, at a jump's peak %v: %s, the body is %g m into the ring, past its cliff, at (%g, %g), %g m up", mv.what, up, peak, when, d, s.X, s.Z, s.Y)
								}
							}
							if peak {
								jump := side.press
								jump.Jump = true
								s = sim.Step(s, jump, g, p)
								for s.VY > 0 {
									s = sim.Step(s, side.press, g, p)
									check("jumping")
								}
							}
							way := sim.Way{X: side.x, Z: side.z, Length: mv.move.Distance}
							switch mv.move.Kind {
							case gamedata.BlinkMove:
								s = sim.Blink(s, way, g, p)
							case gamedata.DashMove, gamedata.LeapMove:
								n := int(math.Round(mv.move.Time.Seconds() / p.Dt))
								s = sim.Dash(s, way, n, sim.SlideOnMobs, mv.move.Kind == gamedata.LeapMove, p)
							}
							check("as it fires")
							for range 120 {
								s = sim.Step(s, side.press, g, p)
								check("pushing on")
							}
						}
					}
				}
			})
		}
	}
}

// climbed is the highest a player stands on the kit's solids from the ground, jumping rise at most from one top to the next.
func climbed(kit regiongen.Kit, rise float64) float64 {
	var tops []float64
	for _, m := range kit.Models {
		switch {
		case m.Cylinder != nil:
			tops = append(tops, m.Cylinder.Height)
		case m.Box != nil:
			tops = append(tops, m.Box.Height)
		}
	}
	slices.Sort(tops)
	at := 0.0
	for _, top := range tops {
		if top <= at+rise {
			at = top
		}
	}
	return at
}

// flatRegion is 128 m of level ground, so its ring's face is as high over the ground by it as over any inside.
func flatRegion() regiongen.Plan {
	return regiongen.Plan{
		Name: "Flat", Size: [2]float64{128, 128}, Cell: 1, Seed: 1, Look: regiongen.Look{Sky: "day"},
		Ground: regiongen.Ground{Noise: regiongen.Noise{Scale: 50, Amplitude: 0, Octaves: 1}},
		Edge:   regiongen.Edge{Width: 12, Height: [2]float64{4, 8}, Biome: "meadow"},
		Colour: regiongen.Colour{Slope: 0.25, High: 100},
		Areas:  []regiongen.Area{{ID: "all", Area: [][2]float64{{12, 12}, {116, 12}, {116, 116}, {12, 116}}, Levels: [2]int{1, 1}, Biome: "meadow", Kind: regiongen.Hunting}},
		Start:  regiongen.Place{At: [2]float64{64, 64}},
		Biomes: map[string]regiongen.Biome{"meadow": {Colours: regiongen.Colours{Flat: "grass", Slope: "dirt", Steep: "stone", High: "snow"}}},
	}
}

// shippedKit is data/kit.yaml.
func shippedKit(t *testing.T) regiongen.Kit {
	t.Helper()
	raw, err := os.ReadFile("../../../data/kit.yaml")
	if err != nil {
		t.Fatal(err)
	}
	k, err := regiongen.DecodeKit(raw)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// A river too deep to wade is crossed at its ford (large-regions.md decision 9): from the west a player reaches the east bank by the ford, and never the deep channel. Nobody fits in it, no path leads into it, and one from inside it, as no body should be, leads out by the nearest dry cell.
func TestADeepRiverIsCrossedAtItsFord(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := data.Movement()
	plan := flatRegion()
	plan.Features = []regiongen.Feature{{River: &regiongen.River{Path: [][2]float64{{64, 0}, {64, 128}}, Width: 6, Depth: 2, Fords: [][2]float64{{64, 40}}}}}
	r, err := regiongen.Generate(plan, shippedKit(t), p.MaxSlope)
	if err != nil {
		t.Fatal(err)
	}
	g := r.Geometry
	grid := nav.New(g, p.Step)
	channel, ford := nav.Point{X: 64, Z: 64}, nav.Point{X: 64, Z: 40}
	if nav.FitsAt(g, p.Step, channel, p.Radius) || grid.Fits(channel, p.Radius) || !nav.FitsAt(g, p.Step, ford, p.Radius) || !grid.Fits(ford, p.Radius) {
		t.Fatal("a body fits in the channel, or not at the ford")
	}
	var f nav.Finder
	bank, in := nav.Point{X: 55, Z: 64}, nav.Point{X: 61.6, Z: 64}
	if path := f.Path(grid, bank, in, p.Radius, bank, 30); path != nil {
		t.Fatalf("a path into the deep water: %v", path)
	}
	out := f.Path(grid, in, bank, p.Radius, bank, 30)
	if len(out) == 0 || out[0].X != 60.75 {
		t.Fatalf("a path out of the deep water: %v", out)
	}
	for _, at := range out {
		if g.Terrain.Deep(at.X, at.Z) {
			t.Fatalf("a path out of the deep water goes by %v, in it", at)
		}
	}
	got := grid.Reach([]nav.Point{{X: 30, Z: 64}}, p.Radius, fastRise(data, p))
	if !got.Has(nav.Point{X: 100, Z: 64}) || !got.Has(nav.Point{X: 64, Z: 40}) {
		t.Fatal("the east bank isn't reached by the ford")
	}
	deep := 0
	for z := 0.25; z < 128; z += 0.5 {
		for x := 0.25; x < 128; x += 0.5 {
			if g.Terrain.Deep(x, z) {
				deep++
				if got.Has(nav.Point{X: x, Z: z}) {
					t.Fatalf("a player reaches (%g, %g), in deep water", x, z)
				}
			}
		}
	}
	if deep < 1000 {
		t.Fatalf("only %d deep cells", deep)
	}
}
