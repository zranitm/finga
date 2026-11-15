package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"finga/internal/gamedata/load"
)

// Each shipped map is written with the geometry the server builds, number for number, its models in step, and nothing of its boss.
func TestEveryMapIsWrittenAsTheServerBuildsIt(t *testing.T) {
	out := filepath.Join(t.TempDir(), "maps")
	if err := run("../../data", out); err != nil {
		t.Fatal(err)
	}
	d, err := load.Dir("../../data")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range d.Maps() {
		raw, err := os.ReadFile(filepath.Join(out, m.ID+".json"))
		if err != nil {
			t.Fatal(err)
		}
		var got Map
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		want := m.Geometry
		if !reflect.DeepEqual(got.Geometry.Terrain.Heights, want.Terrain.Heights) || !reflect.DeepEqual(got.Geometry.Boxes, want.Boxes) ||
			!reflect.DeepEqual(got.Geometry.Cylinders, want.Cylinders) || got.Geometry.SizeX != want.SizeX || got.Geometry.SizeZ != want.SizeZ {
			t.Errorf("%s: the written geometry differs from the server's", m.ID)
		}
		if len(got.BoxModels) != len(want.Boxes) || len(got.CylinderModels) != len(want.Cylinders) || len(got.Portals) != len(m.Portals) {
			t.Errorf("%s: %d box and %d cylinder models, %d portals", m.ID, len(got.BoxModels), len(got.CylinderModels), len(got.Portals))
		}
		if strings.Contains(string(raw), "kills") || m.Boss != nil && strings.Contains(string(raw), m.Boss.Mob) {
			t.Errorf("%s: the boss is written out", m.ID)
		}
	}
}

func TestBadDataWritesNothing(t *testing.T) {
	out := filepath.Join(t.TempDir(), "maps")
	if err := run(t.TempDir(), out); err == nil {
		t.Fatal("no error from an empty data folder")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("wrote %s anyway: %v", out, err)
	}
}
