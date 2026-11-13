package sim_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"finga/internal/sim"
)

// The vectors in testdata/movement must be reproduced exactly here and by the TypeScript port; -update rewrites them.
var update = flag.Bool("update", false, "rewrite the expected states in testdata/movement")

const vectorsDir = "../../testdata/movement"

// vector is one file: a scenario with its inputs and the state after each sub-step.
type vector struct {
	Name        string       `json:"name"`
	Notes       string       `json:"notes,omitempty"`
	SpeedFactor float64      `json:"speed_factor,omitempty"` // unset is 1
	Geometry    sim.Geometry `json:"geometry"`
	Start       sim.State    `json:"start"`
	Inputs      []inputRun   `json:"inputs"`
	Expected    []sim.State  `json:"expected"`
}

// inputRun holds one input for Repeat sub-steps; Blockers, when set, are the mobs from this run on, as they move.
type inputRun struct {
	Input    sim.Input  `json:"input"`
	Repeat   int        `json:"repeat"`
	Blockers []sim.Rect `json:"blockers,omitempty"`
}

// Every scenario must have a file.
var requiredVectors = []string{
	"walk-flat", "jump", "ledge", "land-on-platform", "jump-through-platform",
	"drop-through", "climb-rope", "wall", "ceiling", "platform-edge", "fall-clamp",
	"walk-fast", "climb-rope-fast", "jump-onto-platform-fast", "fall-clamp-fast",
	"walk-casting", "climb-casting", "jump-casting", "casting-runs-out", "walk-casting-fast",
	"blocker-sides", "blocker-walk-out-right", "blocker-walk-out-left", "blocker-jump-over", "blocker-land-through", "blocker-air-side", "blocker-beside-solid", "blocker-snap-rounding", "blocker-partly-in", "blocker-moves-in",
}

func TestVectors(t *testing.T) {
	files, err := filepath.Glob(filepath.Join(vectorsDir, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatalf("no vectors in %s", vectorsDir)
	}
	names := map[string]bool{}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".json")
		names[name] = true
		t.Run(name, func(t *testing.T) {
			v := load(t, file)
			if v.Name != name {
				t.Fatalf("name %q inside the file, want %q", v.Name, name)
			}
			got := replay(v)
			if *update {
				v.Expected = got
				save(t, file, v)
				return
			}
			if len(v.Expected) != len(got) {
				t.Fatalf("%d expected states for %d sub-steps; run with -update after a deliberate rule change", len(v.Expected), len(got))
			}
			for i := range got {
				if got[i] != v.Expected[i] {
					t.Fatalf("sub-step %d: got %+v, want %+v", i+1, got[i], v.Expected[i])
				}
			}
		})
	}
	for _, name := range requiredVectors {
		if !names[name] {
			t.Errorf("missing vector %s.json", name)
		}
	}
}

func replay(v vector) []sim.State {
	p := params()
	if v.SpeedFactor != 0 {
		p = p.Faster(v.SpeedFactor)
	}
	s, g := v.Start, v.Geometry
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

func load(t *testing.T, file string) vector {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var v vector
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%s: %v", file, err)
	}
	return v
}

func save(t *testing.T, file string, v vector) {
	t.Helper()
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestVectorsRoundTripThroughJSON checks floats print in the shortest exact form, as JSON.stringify does.
func TestVectorsRoundTripThroughJSON(t *testing.T) {
	s := sim.State{X: 200 + 200.0/60, Y: 419.99999999999994, VX: 200, VY: -588.3333333333334, OnGround: false, Climbing: true, Facing: -1, Ground: 1, DropPlatform: 0, DropTimer: 0.25 - 1.0/60, Casting: 7}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var back sim.State
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back != s {
		t.Fatalf("round trip changed the state:\n%s\n%+v\n%+v", data, s, back)
	}
	for _, key := range []string{`"x"`, `"y"`, `"vx"`, `"vy"`, `"on_ground"`, `"climbing"`, `"facing"`, `"ground"`, `"drop_platform"`, `"drop_timer"`, `"casting"`} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("JSON lacks %s: %s", key, data)
		}
	}
	keys := []string{"left", "right", "up", "down", "jump"}
	in, _ := json.Marshal(sim.Input{Left: true, Right: true, Up: true, Down: true, Jump: true})
	for _, key := range keys {
		if !strings.Contains(string(in), `"`+key+`"`) {
			t.Fatalf("input JSON lacks %s: %s", key, in)
		}
	}
	if !slices.Contains(requiredVectors, "jump") {
		t.Fatal("unreachable")
	}
}
