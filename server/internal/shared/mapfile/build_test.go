package mapfile

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"finga/internal/shared/sim"
)

var update = flag.Bool("update", false, "rewrite testdata/maps/geometry.sha256 from the shipped maps")

// params are data/movement.yaml's.
var params = sim.Params{Dt: 1.0 / 60, WalkSpeed: 5, JumpVelocity: 12.34, Gravity: 38, MaxFallSpeed: 22.5, Radius: 0.375, Height: 1.75, Step: 0.3, MaxSlope: 0.7, Snap: 0.5}

func TestHill(t *testing.T) {
	h := Hill{At: [2]float64{10, 10}, Radius: 4, Height: 2}
	for _, tt := range []struct{ x, z, want float64 }{
		{10, 10, 2},                 // its top
		{12, 10, 1.125},             // halfway out: 2 × (1 − ¼)²
		{10, 14, 0},                 // its foot
		{20, 20, 0},                 // beyond it
		{10 + 4*0.6, 10 + 4*0.8, 0}, // its foot, diagonally
	} {
		if got := hill(h, tt.x, tt.z); math.Abs(got-tt.want) > 1e-12 {
			t.Errorf("hill at (%g, %g) = %v, want %v", tt.x, tt.z, got, tt.want)
		}
	}
}

func TestMesa(t *testing.T) {
	m := Mesa{Area: [4]float64{10, 20, 4, 2}, Height: 1.5, Edge: 2}
	for _, tt := range []struct{ x, z, want float64 }{
		{12, 21, 1.5},  // on top
		{10, 20, 1.5},  // its corner
		{9, 21, 0.75},  // halfway down its west edge
		{12, 23, 0.75}, // halfway down its north edge
		{14 + math.Sqrt(0.5), 22 + math.Sqrt(0.5), 0.75}, // halfway down its rounded corner
		{7.5, 21, 0}, // past its foot
	} {
		if got := mesa(m, tt.x, tt.z); math.Abs(got-tt.want) > 1e-12 {
			t.Errorf("mesa at (%g, %g) = %v, want %v", tt.x, tt.z, got, tt.want)
		}
	}
}

// A box's axis is (cos yaw, −sin yaw): width across its facing, depth along it, as the sim's box frame reads them.
func TestBoxStandsOnTheGroundTurnedByItsYaw(t *testing.T) {
	box := func(yaw float64) Solid {
		return Solid{Box: &[4]float64{10, 10, 4, 2}, Height: 1, Yaw: yaw, Model: "rock"}
	}
	f := File{Look: Look{Ground: "g", Sky: "s"}, Size: [2]float64{20, 20}, Terrain: Terrain{Cell: 0.5, Hills: []Hill{{At: [2]float64{10, 10}, Radius: 8, Height: 0.5}}},
		Solids: []Solid{box(0), box(90), box(15)}}
	m, err := Build("test", f, params)
	if err != nil {
		t.Fatal(err)
	}
	s15, c15 := math.Sincos(15 * math.Pi / 180)
	for i, want := range [][2]float64{{1, 0}, {0, -1}, {c15, -s15}} {
		b := m.Geometry.Boxes[i]
		if math.Abs(b.UX-want[0]) > 1e-15 || math.Abs(b.UZ-want[1]) > 1e-15 || b.HalfW != 2 || b.HalfD != 1 || b.Top != 1.5 {
			t.Errorf("box %d: %+v, want axis %v, half sizes 2 and 1, top 1.5", i, b, want)
		}
	}
	// Turned a quarter, the 4 m width runs north–south.
	if b := m.Geometry.Boxes[1]; !b.Holds(10.5, 11.9) || b.Holds(11.9, 10.5) {
		t.Error("the box turned 90° doesn't run north–south")
	}
}

// awkward is a map whose every product rounds, where the shipped maps' whole-metre centres would hide a fused one.
var awkward = File{
	Look: Look{Ground: "g", Sky: "s"}, Size: [2]float64{23.5, 17},
	Terrain: Terrain{Cell: 0.5,
		Hills: []Hill{{At: [2]float64{7.3, 5.9}, Radius: 4.7, Height: 1.37}, {At: [2]float64{15.1, 11.3}, Radius: 6.1, Height: 0.83}},
		Mesas: []Mesa{{Area: [4]float64{3.3, 10.1, 4.9, 3.7}, Height: 1.13, Edge: 0.7}, {Area: [4]float64{17.1, 2.3, 3.1, 4.3}, Height: 0.91, Edge: 2.9}}},
	Solids: []Solid{
		{Box: &[4]float64{11.3, 3.7, 2.9, 1.3}, Height: 1.7, Yaw: 27, Model: "m"},
		{Box: &[4]float64{6.1, 13.3, 1.9, 2.3}, Height: 0.29, Yaw: 64.3, Model: "m"},
		{Cylinder: &[2]float64{19.7, 14.1}, Radius: 0.73, Height: 3.3, Model: "m"},
	},
}

// geometryHashes is awkward's and each shipped map's sim geometry, hashed, the same on every CPU; amd64 and arm64 CI both check.
func geometryHashes(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("../../../data/maps/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no maps: %v", err)
	}
	hash := func(id string, f File) string {
		m, err := Build(id, f, params)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		g, err := json.Marshal(m.Geometry)
		if err != nil {
			t.Fatal(err)
		}
		return fmt.Sprintf("%s %x", id, sha256.Sum256(g))
	}
	out := []string{hash("awkward", awkward)}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var f File
		if err := yaml.Unmarshal(raw, &f); err != nil {
			t.Fatal(err)
		}
		out = append(out, hash(strings.TrimSuffix(filepath.Base(path), ".yaml"), f))
	}
	return out
}

func TestShippedGeometryBits(t *testing.T) {
	const golden = "../../../testdata/maps/geometry.sha256"
	got := strings.Join(geometryHashes(t), "\n") + "\n"
	if *update {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("geometry differs (go test ./internal/mapfile -update after a map edit)\ngot:\n%swant:\n%s", got, want)
	}
}

// Maps built alike share one walk grid, built once at the first look; a different geometry or step gets its own.
func TestMapsBuiltAlikeShareTheirGrid(t *testing.T) {
	file := func(height float64) File {
		return File{Look: Look{Ground: "g", Sky: "s"}, Size: [2]float64{20, 20}, Terrain: Terrain{Cell: 0.5},
			Solids: []Solid{{Box: &[4]float64{10, 10, 4, 2}, Height: height, Model: "rock"}}}
	}
	build := func(f File, p sim.Params) *Map {
		t.Helper()
		m, err := Build("test", f, p)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	a, b := build(file(1.25), params), build(file(1.25), params)
	if a.Grid() != b.Grid() || a.Grid() == nil {
		t.Fatal("two builds of one map have their own grids")
	}
	stepped := params
	stepped.Step = 0.5
	if c, d := build(file(2.25), params), build(file(1.25), stepped); c.Grid() == a.Grid() || d.Grid() == a.Grid() || c.Grid() == d.Grid() {
		t.Fatal("a map with other solids or another step shares a grid")
	}
}
