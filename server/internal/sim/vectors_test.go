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

// The vectors in testdata/movement are the contract between this package
// and the TypeScript port: both must reproduce every expected state exactly.
// Run with -update to regenerate the expected states from this package after
// a deliberate change to the rules; the scenario tests in sim_test.go are
// what keeps those states honest.
var update = flag.Bool("update", false, "rewrite the expected states in testdata/movement")

const vectorsDir = "../../testdata/movement"

// vector is one file: a scenario with its inputs and the state after each sub-step.
type vector struct {
	Name     string       `json:"name"`
	Notes    string       `json:"notes,omitempty"`
	Geometry sim.Geometry `json:"geometry"`
	Start    sim.State    `json:"start"`
	Inputs   []inputRun   `json:"inputs"`
	Expected []sim.State  `json:"expected"`
}

// inputRun holds one input for Repeat sub-steps.
type inputRun struct {
	Input  sim.Input `json:"input"`
	Repeat int       `json:"repeat"`
}

// The scenarios the step page lists; every one must have a file.
var requiredVectors = []string{
	"walk-flat", "jump", "ledge", "land-on-platform", "jump-through-platform",
	"drop-through", "climb-rope", "wall", "ceiling", "platform-edge", "fall-clamp",
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
	p := sim.DefaultParams()
	s := v.Start
	var out []sim.State
	for _, run := range v.Inputs {
		for range run.Repeat {
			s = sim.Step(s, run.Input, v.Geometry, p)
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

// TestVectorsRoundTripThroughJSON guards the contract's encoding: every
// state field survives JSON, and a float prints in the shortest form that
// parses back exactly, which is also what JavaScript's JSON.stringify does.
func TestVectorsRoundTripThroughJSON(t *testing.T) {
	s := sim.State{X: 200 + 200.0/60, Y: 419.99999999999994, VX: 200, VY: -588.3333333333334, OnGround: false, Climbing: true, Facing: -1, Ground: 1, DropPlatform: 0, DropTimer: 0.25 - 1.0/60}
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
	for _, key := range []string{`"x"`, `"y"`, `"vx"`, `"vy"`, `"on_ground"`, `"climbing"`, `"facing"`, `"ground"`, `"drop_platform"`, `"drop_timer"`} {
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
