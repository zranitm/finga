package load_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"finga/internal/shared/gamedata/load"
)

func TestMapRejections(t *testing.T) {
	const pit = "maps/pit.yaml"
	tests := []struct {
		name    string
		file    string
		old     string
		replace string
		wantAll []string
	}{
		// Only a match's map is a file; the land is regions.
		{"an unknown kind", pit, "kind: pvp", "kind: dungeon", []string{pit, "dungeon"}},
		{"a town map", pit, "kind: pvp", "kind: town", []string{pit, "town", "pvp"}},
		{"a hunting map", pit, "kind: pvp", "kind: hunting", []string{pit, "hunting", "pvp"}},
		{"a region as a map file", pit, "kind: pvp", "kind: region", []string{pit, "region", "pvp"}},

		// A match's map: its sides and prep wall, and none of the world's.
		{"a pvp map with one side", pit, "  - [{at: [10, 16], yaw: 180}]\n", "", []string{pit, "two sides"}},
		{"a pvp map with an empty side", pit, "  - [{at: [10, 16], yaw: 180}]\n", "  - []\n", []string{pit, "two sides"}},
		{"a pvp map without a prep wall", pit, "prep_wall:\n  - {box: [10, 10, 20, 0.6], height: 3, model: prep_wall}\n", "", []string{pit, "prep_wall"}},
		{"a pvp map with a player spawn", pit, "portals: []", "portals: []\nplayer_spawn: {at: [10, 10], yaw: 0}", []string{pit, "player_spawn"}},
		{"a pvp map that new characters start in", pit, "kind: pvp", "kind: pvp\nstart: true", []string{pit, "start"}},
		{"a pvp map with a portal", pit, "portals: []", "portals:\n  - {name: back, at: [2, 2], radius: 1, to: {map: town, portal: to_vale}}", []string{pit, "portals"}},
		{"a pvp map with a spawn", pit, "portals: []", "portals: []\nspawns:\n  - {name: s, at: [10, 10], radius: 2, mob: green_slime, max: 1, respawn: 1s}", []string{pit, "spawns"}},
		{"a spot inside the prep wall", pit, "{at: [10, 4], yaw: 0}", "{at: [10, 10], yaw: 0}", []string{pit, "side 1", "prep wall"}},
		{"a prep wall off the map", pit, "box: [10, 10, 20, 0.6]", "box: [30, 10, 20, 0.6]", []string{pit, "prep_wall"}},

		// The shapes.
		{"an unknown key", pit, "terrain: {cell: 4}", "terrain: {cell: 4}\nturn: 30", []string{pit, "turn"}},
		{"a look without a sky", pit, "sky: day", "sky: ''", []string{pit, "look"}},
		{"a cell that doesn't divide the size", pit, "size: [20, 20]", "size: [20.3, 20]", []string{pit, "terrain", "cell"}},
		{"a solid that is neither a box nor a cylinder", pit, "box: [10, 10, 20, 0.6], height: 3", "radius: 0.6, height: 3", []string{pit, "prep_wall", "prep_wall", "box or a cylinder"}},
		{"a box with a radius", pit, "box: [10, 10, 20, 0.6], height: 3", "box: [10, 10, 20, 0.6], radius: 1, height: 3", []string{pit, "prep_wall", "radius"}},
		{"a prop both at a spot and a scatter", pit, "terrain: {cell: 4}\n", "terrain: {cell: 4}\nprops:\n  - {at: [2, 2], scatter: {model: m, area: [0, 0, 4, 4], count: 1, seed: 1}}\n", []string{pit, "prop 1"}},
		{"a step of zero", "movement.yaml", "step: 0.3", "step: 0", []string{"movement.yaml", "step"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := fixture()
			before := files[tt.file]
			files[tt.file] = strings.Replace(before, tt.old, tt.replace, 1)
			if files[tt.file] == before {
				t.Fatalf("replacement %q not found in %s", tt.old, tt.file)
			}
			_, err := load.Dir(write(t, files))
			if err == nil {
				t.Fatal("no error")
			}
			for _, want := range tt.wantAll {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

func TestMapRejectionsAcrossFiles(t *testing.T) {
	t.Run("no maps folder", func(t *testing.T) {
		dir := write(t, fixture())
		if err := os.RemoveAll(filepath.Join(dir, "maps")); err != nil {
			t.Fatal(err)
		}
		if _, err := load.Dir(dir); err == nil || !strings.Contains(err.Error(), "maps") {
			t.Fatalf("error %v", err)
		}
	})
}
