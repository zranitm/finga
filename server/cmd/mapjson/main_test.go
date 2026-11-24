package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
)

// Each shipped map is written with the geometry the server builds, number for number, its models in step, its portals' targets, and nothing of its boss; a region with no geometry or models, and its bake named as the server names it, whose chunks hold its ground.
func TestEveryMapIsWrittenAsTheServerBuildsIt(t *testing.T) {
	out := filepath.Join(t.TempDir(), "maps")
	if err := run("../../data", out); err != nil {
		t.Fatal(err)
	}
	d, err := load.Dir("../../data")
	if err != nil {
		t.Fatal(err)
	}
	regions := 0
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
		switch {
		case m.Kind == gamedata.Region:
			if got.Geometry != nil || got.Region == nil || *got.Region != (Region{ID: m.ID, Hash: m.Bake}) || len(got.BoxModels)+len(got.CylinderModels)+len(got.Props) > 0 || got.Sky == "" {
				t.Errorf("%s: region %+v, %d box and %d cylinder models, %d props, sky %q", m.ID, got.Region, len(got.BoxModels), len(got.CylinderModels), len(got.Props), got.Sky)
			}
			var waypoints []Waypoint
			for _, w := range m.Waypoints {
				waypoints = append(waypoints, Waypoint{ID: w.ID, Name: w.Name, At: [2]float64{w.At.X, w.At.Z}, Ground: m.Geometry.Terrain.Height(w.At.X, w.At.Z), Main: w.Main})
			}
			if !reflect.DeepEqual(got.Waypoints, waypoints) || !slices.ContainsFunc(waypoints, func(w Waypoint) bool { return w.Main }) {
				t.Errorf("%s: waypoints %+v; want the server's %+v, a main town's among them", m.ID, got.Waypoints, waypoints)
			}
			regions++
		case got.Region != nil || len(got.Waypoints) > 0 || len(got.BoxModels) != len(want.Boxes) || len(got.CylinderModels) != len(want.Cylinders):
			t.Errorf("%s: region %+v, %d box and %d cylinder models", m.ID, got.Region, len(got.BoxModels), len(got.CylinderModels))
		case !reflect.DeepEqual(got.Geometry.Terrain.Heights, want.Terrain.Heights) || !reflect.DeepEqual(got.Geometry.Boxes, want.Boxes) ||
			!reflect.DeepEqual(got.Geometry.Cylinders, want.Cylinders) || got.Geometry.SizeX != want.SizeX || got.Geometry.SizeZ != want.SizeZ:
			t.Errorf("%s: the written geometry differs from the server's", m.ID)
		}
		if len(got.Portals) != len(m.Portals) {
			t.Errorf("%s: %d portals", m.ID, len(got.Portals))
		}
		for k, p := range got.Portals {
			if k < len(m.Portals) && (p.Name != m.Portals[k].Name || p.To != m.Portals[k].TargetMap) {
				t.Errorf("%s: portal %+v; want %s to %s", m.ID, p, m.Portals[k].Name, m.Portals[k].TargetMap)
			}
		}
		if strings.Contains(string(raw), "kills") || m.Boss != nil && strings.Contains(string(raw), m.Boss.Mob) {
			t.Errorf("%s: the boss is written out", m.ID)
		}
	}
	if regions == 0 {
		t.Error("no region written")
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
