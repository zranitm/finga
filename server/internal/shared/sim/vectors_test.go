package sim_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"finga/internal/shared/sim"
)

// The vectors in testdata/movement are made here from the scenarios below and replayed exactly by web/src/sim; -update rewrites them.
var update = flag.Bool("update", false, "rewrite testdata/movement from the scenarios")

const vectorsDir = "../../../testdata/movement"

// vector is one file: a scenario with its params, inputs and the state after each sub-step.
type vector struct {
	Name        string       `json:"name"`
	Notes       string       `json:"notes"`
	Params      sim.Params   `json:"params"`
	SpeedFactor float64      `json:"speed_factor,omitempty"` // unset is 1
	Geometry    sim.Geometry `json:"geometry"`
	Start       sim.State    `json:"start"`
	Inputs      []inputRun   `json:"inputs"`
	Expected    []sim.State  `json:"expected"`
}

// inputRun holds one input for Repeat sub-steps; Blockers, when set, are the mobs from this run on.
type inputRun struct {
	Input    sim.Input     `json:"input"`
	Repeat   int           `json:"repeat"`
	Blockers []sim.Blocker `json:"blockers,omitempty"`
}

// The scenarios of docs/tech/3d-world.md §5, each a file.
var requiredVectors = []string{
	"walk-north", "walk-north-east", "walk-east", "walk-south-east", "walk-south", "walk-south-west", "walk-west", "walk-north-west",
	"walk-opposite-keys", "walk-fast",
	"slide-box", "box-corner", "slide-box-rotated", "box-corner-rotated", "slide-cylinder", "round-cylinder", "round-cylinder-east", "cylinder-middle",
	"ramp-up-axis", "ramp-up-diagonal", "cliff-stop-axis", "cliff-stop-diagonal", "cliff-straddles-cells",
	"jump-onto-cliff", "jump-short-of-cliff", "step-off-cliff", "land-on-steep-cell", "walk-down-slope",
	"jump", "jump-onto-box", "jump-short-of-box", "jump-onto-stump", "jump-across-step", "step-onto-box",
	"fall-off-mesa", "fall-clamp",
	"mob-walk-into", "mob-walk-out", "mob-walk-along", "mob-jump-over",
	"map-edge",
}

// params are §3's starting values in metres.
var params = sim.Params{
	Dt:           1.0 / 60,
	WalkSpeed:    5,
	JumpVelocity: 12.34,
	Gravity:      38,
	MaxFallSpeed: 22.5,
	Radius:       0.375,
	Height:       1.75,
	Step:         0.3,
	MaxSlope:     0.7,
	Snap:         0.5,
}

var (
	east, west, north, south = sim.Input{Right: true}, sim.Input{Left: true}, sim.Input{Up: true}, sim.Input{Down: true}
	northEast, southEast     = sim.Input{Up: true, Right: true}, sim.Input{Down: true, Right: true}
	jumpEast, still          = sim.Input{Right: true, Jump: true}, sim.Input{}
)

// ground is a sizeX by sizeZ map whose terrain samples, cell apart, are h(x, z).
func ground(sizeX, sizeZ, cell float64, h func(x, z float64) float64) sim.Geometry {
	nx, nz := int(sizeX/cell)+1, int(sizeZ/cell)+1
	heights := make([]float64, 0, nx*nz)
	for iz := range nz {
		for ix := range nx {
			heights = append(heights, h(float64(ix)*cell, float64(iz)*cell))
		}
	}
	return sim.Geometry{
		SizeX: sizeX, SizeZ: sizeZ,
		Terrain:   sim.Terrain{Cell: cell, NX: nx, NZ: nz, Heights: heights},
		Boxes:     []sim.Box{},
		Cylinders: []sim.Cylinder{},
	}
}

func flat() sim.Geometry { return ground(12, 12, 12, func(_, _ float64) float64 { return 0 }) }

// rising is ground 12 by 6 that is low west of from, rises to top at to, and stays there.
func rising(from, to, top float64) sim.Geometry {
	return ground(12, 6, 0.5, func(x, _ float64) float64 {
		switch {
		case x <= from:
			return 0
		case x >= to:
			return top
		}
		return top * (x - from) / (to - from)
	})
}

func withBoxes(g sim.Geometry, boxes ...sim.Box) sim.Geometry { g.Boxes = boxes; return g }

func standing(x, y, z float64) sim.State {
	return sim.State{X: x, Y: y, Z: z, OnGround: true, HZ: 1}
}

func runs(rs ...any) []inputRun {
	var out []inputRun
	for i := 0; i < len(rs); i += 2 {
		out = append(out, inputRun{Input: rs[i].(sim.Input), Repeat: rs[i+1].(int)})
	}
	return out
}

// rotated is a box at (8, 6), 3 by 2, turned so its long side runs along (0.6, 0.8).
var rotated = sim.Box{X: 8, Z: 6, HalfW: 1.5, HalfD: 1, UX: 0.6, UZ: 0.8, Top: 2}

// trunk is flat ground with a tree trunk at (8, 6), 1 m across the radius.
func trunk() sim.Geometry {
	g := flat()
	g.Cylinders = []sim.Cylinder{{X: 8, Z: 6, Radius: 1, Top: 7}}
	return g
}

// mob is a slime's circle at (8, 6), 1 m tall.
var mob = []sim.Blocker{{X: 8, Z: 6, Radius: 0.5, Bottom: 0, Top: 1}}

func scenarios() []vector {
	walk := func(name, way string, in sim.Input) vector {
		return vector{Name: name, Notes: "Walking " + way + " from the middle of flat ground, then letting go: the heading stays.", Geometry: flat(), Start: standing(6, 0, 6), Inputs: runs(in, 30, still, 5)}
	}
	vs := []vector{
		walk("walk-north", "north", north),
		walk("walk-north-east", "north-east, √½ on each axis", northEast),
		walk("walk-east", "east", east),
		walk("walk-south-east", "south-east", southEast),
		walk("walk-south", "south", south),
		walk("walk-south-west", "south-west", sim.Input{Down: true, Left: true}),
		walk("walk-west", "west", west),
		walk("walk-north-west", "north-west", sim.Input{Up: true, Left: true}),
		{Name: "walk-opposite-keys", Notes: "Opposite keys cancel and keep the heading; with a third key held, it alone walks.", Geometry: flat(), Start: sim.State{X: 6, Z: 6, OnGround: true, HX: 1},
			Inputs: runs(sim.Input{Left: true, Right: true}, 10, sim.Input{Up: true, Down: true}, 10, sim.Input{Left: true, Right: true, Up: true, Down: true}, 10,
				sim.Input{Left: true, Right: true, Up: true}, 10, sim.Input{Up: true, Down: true, Right: true}, 10)},
		{Name: "walk-fast", Notes: "Walking north-east at a move speed factor of 1.3.", SpeedFactor: 1.3, Geometry: flat(), Start: standing(4, 0, 4), Inputs: runs(northEast, 30)},
		{Name: "slide-box", Notes: "Walking north-east into a box's west face: x stops at contact and z slides along it, then round its corner.",
			Geometry: withBoxes(flat(), sim.Box{X: 8, Z: 6, HalfW: 1, HalfD: 3, UX: 1, Top: 2}), Start: standing(5, 0, 3), Inputs: runs(northEast, 110)},
		{Name: "box-corner", Notes: "Walking east on a line 0.2 m south of a box's south-west corner stops on the rounded corner, further east than the face stops it; south-east then slides off it.",
			Geometry: withBoxes(flat(), sim.Box{X: 8, Z: 6, HalfW: 1, HalfD: 3, UX: 1, Top: 2}), Start: standing(5, 0, 2.8), Inputs: runs(east, 40, southEast, 20, east, 30)},
		{Name: "slide-box-rotated", Notes: "Walking north-east into the middle of a box turned to (0.6, 0.8): x stops at its slanted face each sub-step and z, moving away from it, slides the body along it and past its corner.",
			Geometry: withBoxes(flat(), rotated), Start: standing(4.5, 0, 4.4), Inputs: runs(northEast, 100)},
		{Name: "box-corner-rotated", Notes: "Walking east just inside the turned box's west corner at (6.3, 5.4) stops on it; south-east then slides off it.",
			Geometry: withBoxes(flat(), rotated), Start: standing(4, 0, 5.2), Inputs: runs(east, 40, southEast, 40)},
		{Name: "slide-cylinder", Notes: "Walking north-east into a tree trunk: the body slides round it.",
			Geometry: func() sim.Geometry {
				g := flat()
				g.Cylinders = []sim.Cylinder{{X: 8, Z: 6, Radius: 1, Top: 7}}
				return g
			}(), Start: standing(5, 0, 4.5), Inputs: runs(northEast, 90)},
		{Name: "round-cylinder", Notes: "Walking north-east at a trunk, meeting it near its middle: both keys' moves stop at its rim, so the body slides round it along the tangent, then walks on.",
			Geometry: trunk(), Start: standing(5, 0, 3.4), Inputs: runs(northEast, 120)},
		{Name: "round-cylinder-east", Notes: "Walking east at a trunk 0.3 m north of its middle: x stops at its rim, and the body slides north round it, slowly at first, then on east.",
			Geometry: trunk(), Start: standing(5, 0, 6.3), Inputs: runs(east, 95)},
		{Name: "cylinder-middle", Notes: "Walking east straight at a trunk's middle: nothing of the move runs along its tangent, so the body stays at its rim.",
			Geometry: trunk(), Start: standing(5, 0, 6), Inputs: runs(east, 60)},
		{Name: "ramp-up-axis", Notes: "Walking east up a 0.5 rise per metre, under max_slope, onto the flat top 2 m up.", Geometry: rising(4, 8, 2), Start: standing(2, 0, 3), Inputs: runs(east, 110)},
		{Name: "ramp-up-diagonal", Notes: "Walking north-east up the same ramp.", Geometry: rising(4, 8, 2), Start: standing(2, 0, 0.5), Inputs: runs(northEast, 110)},
		{Name: "cliff-stop-axis", Notes: "Walking east into a 1.0 rise per metre, over max_slope: the steep cell is a wall.", Geometry: rising(6, 7, 1), Start: standing(4, 0, 3), Inputs: runs(east, 50)},
		{Name: "cliff-stop-diagonal", Notes: "Walking north-east into the same cliff: x stops and z walks on along its foot.", Geometry: rising(6, 7, 1), Start: standing(4, 0, 1), Inputs: runs(northEast, 60)},
		{Name: "cliff-straddles-cells", Notes: "A 0.8 m cliff between 6.25 and 6.75 is spread by the samples across two cells, each rising 0.4 in 0.5 m: both steep.",
			Geometry: rising(6.25, 6.75, 0.8), Start: standing(4, 0, 3), Inputs: runs(east, 50)},
		{Name: "jump-onto-cliff", Notes: "A jump east from the foot of a 1.2 m cliff: in the air its steep cell is a wall until the feet reach its top, then the body moves in and lands on top.",
			Geometry: rising(6, 6.5, 1.2), Start: standing(5.9, 0, 3), Inputs: runs(jumpEast, 1, east, 40)},
		{Name: "jump-short-of-cliff", Notes: "The same jump at a 2.5 m cliff: the feet never reach its top, so the body never enters its steep cell and comes down at its foot.",
			Geometry: rising(6, 6.5, 2.5), Start: standing(5.9, 0, 3), Inputs: runs(jumpEast, 1, east, 40)},
		{Name: "step-off-cliff", Notes: "Walking west off the top of a 1.2 m cliff: into its steep cell the body doesn't snap down but falls, and lands at its foot.",
			Geometry: rising(6, 6.5, 1.2), Start: standing(7.5, 1.2, 3), Inputs: runs(west, 40)},
		{Name: "land-on-steep-cell", Notes: "Dropped onto the face of a 1.2 m cliff, a body with no keys held lands and stands still.",
			Geometry: rising(6, 6.5, 1.2), Start: sim.State{X: 6.25, Y: 2.5, Z: 3, HZ: 1}, Inputs: runs(still, 40)},
		{Name: "walk-down-slope", Notes: "Walking west down the ramp: the ground falls under 0.5 m a sub-step, so the body follows it and stays on the ground.", Geometry: rising(4, 8, 2), Start: standing(10, 2, 3), Inputs: runs(west, 110)},
		{Name: "jump", Notes: "A standing jump on flat ground.", Geometry: flat(), Start: standing(6, 0, 6), Inputs: runs(sim.Input{Jump: true}, 1, still, 45)},
		{Name: "jump-onto-box", Notes: "A jump east onto a box 1.2 m tall, higher than a step: blocked below its top plus a step, landed on above.",
			Geometry: withBoxes(flat(), sim.Box{X: 8, Z: 6, HalfW: 1, HalfD: 1, UX: 1, Top: 1.2}), Start: standing(5, 0, 6), Inputs: runs(east, 8, jumpEast, 1, east, 30)},
		{Name: "jump-short-of-box", Notes: "The same jump at a box 2.5 m tall: the feet never rise above its top less a step.",
			Geometry: withBoxes(flat(), sim.Box{X: 8, Z: 6, HalfW: 1, HalfD: 1, UX: 1, Top: 2.5}), Start: standing(5, 0, 6), Inputs: runs(east, 8, jumpEast, 1, east, 45)},
		{Name: "jump-onto-stump", Notes: "A jump east onto a stump, a cylinder 1 m tall: blocked below its top plus a step, then stood on once the feet's centre is over it.",
			Geometry: func() sim.Geometry {
				g := flat()
				g.Cylinders = []sim.Cylinder{{X: 8, Z: 6, Radius: 0.6, Top: 1}}
				return g
			}(), Start: standing(6, 0, 6), Inputs: runs(jumpEast, 1, east, 25, still, 10)},
		{Name: "jump-across-step", Notes: "Jumping as the body walks onto a box 0.25 m tall: rising past its lip lifts the feet onto it without ending the jump, and it lands on top.",
			Geometry: withBoxes(flat(), sim.Box{X: 8, Z: 6, HalfW: 2.5, HalfD: 1, UX: 1, Top: 0.25}), Start: standing(5.45, 0, 6), Inputs: runs(jumpEast, 1, east, 40)},
		{Name: "step-onto-box", Notes: "A box 0.25 m tall, within a step: walked onto once the feet's centre is over it, and walked off the far side.",
			Geometry: withBoxes(flat(), sim.Box{X: 8, Z: 6, HalfW: 1, HalfD: 1, UX: 1, Top: 0.25}), Start: standing(5, 0, 6), Inputs: runs(east, 80)},
		{Name: "fall-off-mesa", Notes: "Walking west off a mesa 1.5 m high: the ground falls more than snap, so the body falls and lands.", Geometry: rising(6, 6.5, 1.5), Start: standing(9, 1.5, 3), Inputs: runs(west, 60)},
		{Name: "fall-clamp", Notes: "Falling 30 m: the speed stops growing at max_fall_speed, then the body lands.", Geometry: flat(), Start: sim.State{X: 6, Y: 30, Z: 6, HZ: 1}, Inputs: runs(still, 110)},
		{Name: "mob-walk-into", Notes: "Walking east into a mob's circle stops at contact.", Geometry: flat(), Start: standing(5, 0, 6), Inputs: []inputRun{{Input: east, Repeat: 50, Blockers: mob}}},
		{Name: "mob-walk-out", Notes: "A body overlapping a mob can't move closer, east, and walks out, west.", Geometry: flat(), Start: standing(7.6, 0, 6),
			Inputs: []inputRun{{Input: east, Repeat: 10, Blockers: mob}, {Input: west, Repeat: 20}}},
		{Name: "mob-walk-along", Notes: "Walking north-east at a mob, meeting it north of west: x stops at contact while z, moving away, slides the body round it.", Geometry: flat(), Start: standing(5, 0, 4.05), Inputs: []inputRun{{Input: northEast, Repeat: 70, Blockers: mob}}},
		{Name: "mob-jump-over", Notes: "A jump clears a mob 1 m tall, passes over it and lands beyond.", Geometry: flat(), Start: standing(5.5, 0, 6), Inputs: []inputRun{{Input: east, Repeat: 6, Blockers: mob}, {Input: jumpEast, Repeat: 1}, {Input: east, Repeat: 50}}},
		{Name: "map-edge", Notes: "The map's west edge stops the body at its radius; south-west then slides it into the corner.", Geometry: flat(), Start: standing(1, 0, 1.5), Inputs: runs(west, 15, sim.Input{Down: true, Left: true}, 30)},
	}
	for i := range vs {
		vs[i].Params = params
	}
	return vs
}

func TestVectors(t *testing.T) {
	scenarios := scenarios()
	names := map[string]bool{}
	for _, v := range scenarios {
		names[v.Name] = true
		t.Run(v.Name, func(t *testing.T) {
			v.Expected = replay(v)
			want, err := json.MarshalIndent(v, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, '\n')
			file := filepath.Join(vectorsDir, v.Name+".json")
			if *update {
				if err := os.WriteFile(file, want, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%v; run with -update", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("%s differs from its scenario replayed: %s; run with -update after a deliberate change", file, firstDifference(got, v))
			}
		})
	}
	for _, name := range requiredVectors {
		if !names[name] {
			t.Errorf("no scenario %s", name)
		}
	}
	files, err := filepath.Glob(filepath.Join(vectorsDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if name := strings.TrimSuffix(filepath.Base(file), ".json"); !names[name] {
			t.Errorf("%s has no scenario", file)
		}
	}
	if len(names) != len(requiredVectors) {
		t.Errorf("%d scenarios, %d required: list each in requiredVectors", len(names), len(requiredVectors))
	}
}

// firstDifference names the first sub-step whose state the file gets wrong, or says the scenario itself changed.
func firstDifference(file []byte, v vector) string {
	var saved vector
	if err := json.Unmarshal(file, &saved); err != nil {
		return err.Error()
	}
	for i := range min(len(saved.Expected), len(v.Expected)) {
		if saved.Expected[i] != v.Expected[i] {
			return fmt.Sprintf("sub-step %d: the file has %+v, the replay %+v", i+1, saved.Expected[i], v.Expected[i])
		}
	}
	if len(saved.Expected) != len(v.Expected) {
		return fmt.Sprintf("%d states in the file, %d replayed", len(saved.Expected), len(v.Expected))
	}
	return "the scenario changed"
}

func replay(v vector) []sim.State {
	p := v.Params
	if v.SpeedFactor != 0 {
		p = p.Faster(v.SpeedFactor)
	}
	g := v.Geometry
	g.Terrain = sim.NewTerrain(g.Terrain.Cell, g.Terrain.NX, g.Terrain.NZ, g.Terrain.Heights, p.MaxSlope)
	s := v.Start
	var out []sim.State
	for _, run := range v.Inputs {
		if run.Blockers != nil {
			g.Blockers = run.Blockers
		}
		for range run.Repeat {
			s = sim.Step(s, run.Input, g, p)
			out = append(out, s)
		}
	}
	return out
}

// The required list and the scenarios name the same vectors, each once.
func TestRequiredVectorsAreDistinct(t *testing.T) {
	sorted := slices.Clone(requiredVectors)
	slices.Sort(sorted)
	if len(slices.Compact(sorted)) != len(requiredVectors) {
		t.Fatal("a required vector is listed twice")
	}
}
