package regiongen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The lakes in testdata/water/lakes.json, each with its surface's ring as grown here; web/src/game/world3d/water.test.ts grows the same areas and must land on them; -update rewrites the file.
const lakesFile = "../../../testdata/water/lakes.json"

// grownLake is a lake's area and its surface's ring.
type grownLake struct {
	Name string       `json:"name"`
	Area [][2]float64 `json:"area"`
	Ring [][2]float64 `json:"ring"`
}

// grownLakes are the areas: corners either way round, a corner turned in, and one sharp enough that its reach is capped.
var grownLakes = []grownLake{
	{Name: "a square", Area: [][2]float64{{0, 0}, {20, 0}, {20, 20}, {0, 20}}},
	{Name: "an L running clockwise", Area: [][2]float64{{0, 0}, {0, 30}, {10, 30}, {10, 10}, {30, 10}, {30, 0}}},
	{Name: "a sharp corner", Area: [][2]float64{{0, 0}, {40, 1}, {0, 2}}},
}

func TestALakeGrowsAsTheClientDrawsIt(t *testing.T) {
	if *update {
		lines := make([]string, len(grownLakes))
		for i := range grownLakes {
			grownLakes[i].Ring = grown(grownLakes[i].Area)
			b, err := json.Marshal(grownLakes[i])
			if err != nil {
				t.Fatal(err)
			}
			lines[i] = "  " + string(b)
		}
		if err := os.MkdirAll(filepath.Dir(lakesFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(lakesFile, []byte("[\n"+strings.Join(lines, ",\n")+"\n]\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	b, err := os.ReadFile(lakesFile)
	if err != nil {
		t.Fatalf("%v (go test -run TestALakeGrowsAsTheClientDrawsIt -update writes it)", err)
	}
	var want []grownLake
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(grownLakes) {
		t.Fatalf("the file holds %d lakes, the test %d: rewrite it with -update", len(want), len(grownLakes))
	}
	for i, w := range want {
		if w.Name != grownLakes[i].Name || !slices.Equal(w.Area, grownLakes[i].Area) {
			t.Fatalf("lake %d in the file is %q, the test's %q: rewrite it with -update", i, w.Name, grownLakes[i].Name)
		}
		if got := grown(w.Area); !slices.Equal(got, w.Ring) {
			t.Errorf("%s grows to %v, the file's %v", w.Name, got, w.Ring)
		}
	}
}
