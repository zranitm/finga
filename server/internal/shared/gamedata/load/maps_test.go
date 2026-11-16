package load_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"finga/internal/gamedata/load"
)

func TestMapRejections(t *testing.T) {
	const town, field = "maps/town.yaml", "maps/field.yaml"
	tests := []struct {
		name    string
		file    string
		old     string
		replace string
		wantAll []string
	}{
		// The map's own rules, as the 2D maps have them.
		{"an unknown kind", field, "kind: hunting", "kind: dungeon", []string{field, "dungeon"}},
		{"a town with levels", town, "kind: town\n", "kind: town\nlevels: [1, 2]\n", []string{town, "levels"}},
		{"a town with a spawn", town, "npcs:", "spawns:\n  - {name: s, at: [20, 6], radius: 2, mob: green_slime, max: 1, respawn: 1s}\nnpcs:", []string{town, "spawn"}},
		{"a hunting map without a town", field, "town: town\n", "", []string{field, "town"}},
		{"levels out of order", field, "levels: [1, 5]", "levels: [5, 1]", []string{field, "levels"}},
		{"a hunting map without a boss", field, "boss: {at: [30, 14], radius: 4, mob: slime_lord, kills: 20}\n", "", []string{field, "boss"}},
		{"duplicate portal names", town, "portals:\n", "portals:\n  - {name: to_field, at: [20, 6], radius: 1.2, to: {map: field, portal: from_town}}\n", []string{town, "duplicate", "to_field"}},
		{"a spawn of an unknown mob", field, "mob: green_slime", "mob: dragon", []string{field, "slimes", "dragon"}},
		{"a spawn of a boss", field, "mob: green_slime", "mob: slime_lord", []string{field, "slimes", "slime_lord", "boss"}},
		{"a spawn outside the map's levels", field, "levels: [1, 5]", "levels: [2, 5]", []string{field, "slimes", "green_slime", "level"}},
		{"a spawn of none", field, "max: 4", "max: 0", []string{field, "slimes", "max"}},
		{"a boss that isn't one", field, "mob: slime_lord", "mob: green_slime", []string{field, "green_slime", "boss"}},
		{"a boss below the map's top level", field, "levels: [1, 5]", "levels: [1, 6]", []string{field, "slime_lord", "level"}},
		{"a boss brought by no kills", field, "kills: 20", "kills: 0", []string{field, "slime_lord", "kills"}},
		{"a boss circle narrower than its body", field, "radius: 4, mob: slime_lord", "radius: 1, mob: slime_lord", []string{field, "slime_lord", "narrower"}},

		// The shapes.
		{"an unknown key", field, "yaw: 30", "turn: 30", []string{field, "turn"}},
		{"a look without a sky", town, "sky: day", "sky: ''", []string{town, "look"}},
		{"a cell that doesn't divide the size", field, "size: [40, 20]", "size: [40.3, 20]", []string{field, "terrain", "cell"}},
		{"a mesa with no edge", field, "edge: 0.5", "edge: 0", []string{field, "mesa 1", "edge"}},
		{"a solid that is neither a box nor a cylinder", field, "cylinder: [20, 4], radius: 0.6", "radius: 0.6", []string{field, "solid 2", "tree", "box or a cylinder"}},
		{"a box with a radius", field, "box: [14, 14, 4, 2], height: 2", "box: [14, 14, 4, 2], radius: 1, height: 2", []string{field, "solid 1", "rock", "radius"}},
		{"a solid outside the map", field, "cylinder: [20, 4]", "cylinder: [50, 4]", []string{field, "solid 2", "inside the map"}},
		{"a prop both at a spot and a scatter", town, "terrain: {cell: 4}\n", "terrain: {cell: 4}\nprops:\n  - {at: [2, 2], scatter: {model: m, area: [0, 0, 4, 4], count: 1, seed: 1}}\n", []string{town, "prop 1"}},

		// The spots.
		{"a spawn circle outside the map", field, "at: [12, 6], radius: 4", "at: [1, 6], radius: 4", []string{field, "slimes", "inside the map"}},
		{"a spawn over a portal", field, "at: [12, 6], radius: 4", "at: [5, 10], radius: 4", []string{field, "slimes", "from_town", "portal"}},
		{"a boss circle over a portal", field, "at: [30, 14], radius: 4", "at: [6, 10], radius: 4", []string{field, "slime_lord", "from_town", "portal"}},
		{"a portal inside a rock", field, "at: [2, 10], radius: 1.2", "at: [14, 14], radius: 1.2", []string{field, "from_town", "fits"}},
		{"a player spawn inside a rock", field, "player_spawn: {at: [3, 10]", "player_spawn: {at: [14, 14]", []string{field, "player_spawn"}},
		{"an NPC up a tree", field, "id: drillmaster, at: [28, 5]", "id: drillmaster, at: [20, 4]", []string{field, "drillmaster", "fits"}},
		{"an NPC not in npcs.yaml", field, "id: hunter", "id: nobody", []string{field, "nobody", "npcs.yaml"}},

		// Across maps.
		{"a portal to no map", town, "to: {map: field", "to: {map: swamp", []string{town, "to_field", "swamp"}},
		{"a portal to no portal", town, "portal: from_town", "portal: from_nowhere", []string{town, "to_field", "from_nowhere"}},
		{"a hunting map whose town isn't one", field, "town: town", "town: field", []string{field, "town", "field"}},
		{"an NPC on two maps", town, "  - {id: guide, at: [6, 6], yaw: 180}\n", "  - {id: guide, at: [6, 6], yaw: 180}\n  - {id: hunter, at: [9, 6], yaw: 180}\n", []string{"hunter", "already stands"}},
		{"an NPC on no map", town, "  - {id: guide, at: [6, 6], yaw: 180}\n", "", []string{"guide", "no map"}},
		{"no start town", town, "start: true\n", "", []string{"maps", "start"}},
		{"a hunting map that new characters start in", field, "kind: hunting\n", "kind: hunting\nstart: true\n", []string{field, "start"}},
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
	t.Run("one boss on two maps", func(t *testing.T) {
		files := fixture()
		second := strings.NewReplacer("name: from_town", "name: from_town_2", "  - {id: hunter, at: [20, 12], yaw: 180}\n", "", "  - {id: drillmaster, at: [28, 5], yaw: 180}\n", "").Replace(files["maps/field.yaml"])
		files["maps/field2.yaml"] = strings.Replace(second, "npcs:\n", "", 1)
		_, err := load.Dir(write(t, files))
		if err == nil || !strings.Contains(err.Error(), "slime_lord") || !strings.Contains(err.Error(), "already on") {
			t.Fatalf("error %v", err)
		}
	})
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
