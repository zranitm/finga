package load_test

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/nav"
	"finga/internal/shared/regiongen"
	"finga/internal/shared/sim"
)

// vale is a 128 m region of the fixture's mobs, a hamlet south of a meadow, with a gate to the fixture's town and back.
const vale = `
name: Vale
size: [128, 128]
cell: 2
seed: 3
look: {sky: day}
ground: {noise: {scale: 60, amplitude: 1, octaves: 2}}
edge: {width: 16, height: [6, 9], biome: meadow}
colour: {slope: 0.25, high: 20}
areas:
  - {id: hamlet, name: Hamlet, area: [[16, 16], [112, 16], [112, 48], [16, 48]], biome: meadow, kind: town}
  - {id: meadow, name: Meadow, area: [[16, 48], [112, 48], [112, 112], [16, 112]], levels: [1, 5], biome: meadow, kind: hunting,
     boss: {at: [90, 90], radius: 4, mob: slime_lord}}
camps:
  - {area: meadow, name: slimes, at: [40, 80], radius: 5, mob: green_slime, max: 3, respawn: 5s}
waypoints:
  - {id: hamlet, name: Hamlet, at: [64, 32]}
pieces:
  - {house: {area: [80, 24, 6, 6], height: 4, model: house_small}}
gates:
  - {name: west, at: [24, 64], radius: 1.6, to: {region: town, gate: to_vale}}
start: {at: [64, 28], yaw: 90}
biomes:
  meadow:
    colours: {flat: grass, slope: dirt, steep: stone, high: snow}
`

// regioned is the fixture with Vale, the shipped kit it's built with, and the town's portal to Vale's gate.
func regioned(t *testing.T) map[string]string {
	t.Helper()
	kit, err := os.ReadFile(filepath.Join(gamedatatest.Dir(), "kit.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	files := fixture()
	files["kit.yaml"] = string(kit)
	files["regions/vale.yaml"] = vale
	files["maps/town.yaml"] = strings.Replace(files["maps/town.yaml"], "portals:\n", "portals:\n  - {name: to_vale, at: [20, 10], radius: 1.2, to: {map: vale, portal: west}}\n", 1)
	return files
}

// Loads of one plan, kit and slope share its ground, so stacks in one process generate a region once; a plan changed by its seed gets its own.
func TestLoadsOfOnePlanShareItsGround(t *testing.T) {
	files := regioned(t)
	ground := func(files map[string]string) (sim.Geometry, string) {
		d, err := load.Dir(write(t, files))
		if err != nil {
			t.Fatal(err)
		}
		m, _ := d.Map("vale")
		return m.Geometry, m.Bake
	}
	a, aBake := ground(files)
	b, bBake := ground(files)
	if &a.Terrain.Heights[0] != &b.Terrain.Heights[0] || aBake != bBake {
		t.Error("two loads of one plan generated it twice")
	}
	files["regions/vale.yaml"] = strings.Replace(vale, "seed: 3\n", "seed: 4\n", 1)
	c, cBake := ground(files)
	if &c.Terrain.Heights[0] == &a.Terrain.Heights[0] || cBake == aBake || reflect.DeepEqual(c.Terrain.Heights, a.Terrain.Heights) {
		t.Error("another seed read the first plan's ground")
	}
}

func TestARegionLoadsAsAMapOfItsPlan(t *testing.T) {
	files := regioned(t)
	files["maps/field.yaml"] = strings.Replace(files["maps/field.yaml"], "  - {id: hunter, at: [20, 12], yaw: 180}\n", "", 1)
	files["regions/vale.yaml"] = strings.Replace(vale, "start:", "npcs:\n  - {id: hunter, at: [70, 40], yaw: 180}\nstart:", 1)
	d, err := load.Dir(write(t, files))
	if err != nil {
		t.Fatal(err)
	}
	m, ok := d.Map("vale")
	if !ok {
		t.Fatal("no vale")
	}
	t.Run("its spots are its plan's: camps as spawns, gates as portals, the start as the player spawn; and it's its own town", func(t *testing.T) {
		if m.Kind != gamedata.Region || m.Name != "Vale" || m.Town != "vale" || m.Start || m.MinLevel != 0 || m.MaxLevel != 0 || m.Boss != nil {
			t.Fatalf("vale %+v", m)
		}
		if m.PlayerSpawn != (gamedata.Point{X: 64, Z: 28}) || m.SpawnYaw != math.Pi/2 {
			t.Errorf("player spawn %v facing %v", m.PlayerSpawn, m.SpawnYaw)
		}
		if want := []gamedata.Spawn{{Mob: "green_slime", Max: 3, Respawn: 5 * time.Second, At: gamedata.Point{X: 40, Z: 80}, Radius: 5}}; !reflect.DeepEqual(m.Spawns, want) {
			t.Errorf("spawns %+v", m.Spawns)
		}
		if want := []gamedata.Portal{{Name: "west", At: gamedata.Point{X: 24, Z: 64}, Radius: 1.6, TargetMap: "town", TargetPortal: "to_vale"}}; !reflect.DeepEqual(m.Portals, want) {
			t.Errorf("portals %+v", m.Portals)
		}
		if want := []gamedata.Placement{{NPC: "hunter", At: gamedata.Point{X: 70, Z: 40}, Yaw: math.Pi}}; !reflect.DeepEqual(m.NPCs, want) {
			t.Errorf("npcs %+v", m.NPCs)
		}
		if n, _ := d.NPC("hunter"); n.MapID != "vale" || n.At != (gamedata.Point{X: 70, Z: 40}) {
			t.Errorf("the hunter stands on %s at %v", n.MapID, n.At)
		}
	})
	t.Run("its ground is the generator's, its walk grid built over it, and its bake named by the bake's folder", func(t *testing.T) {
		p, err := regiongen.Decode([]byte(files["regions/vale.yaml"]))
		if err != nil {
			t.Fatal(err)
		}
		k, err := regiongen.DecodeKit([]byte(files["kit.yaml"]))
		if err != nil {
			t.Fatal(err)
		}
		r, err := regiongen.Generate(p, k, d.Movement().MaxSlope)
		if err != nil {
			t.Fatal(err)
		}
		b, err := r.Bake()
		if err != nil {
			t.Fatal(err)
		}
		if m.Bake != b.Folder() || len(m.Bake) != 16 {
			t.Errorf("bake %q; want %q", m.Bake, b.Folder())
		}
		if !reflect.DeepEqual(m.Geometry, r.Geometry) {
			t.Error("its geometry isn't the generator's")
		}
		if !m.Grid().Fits(nav.Point{X: 64, Z: 28}, d.Movement().Radius) || m.Grid().Fits(nav.Point{X: 83, Z: 27}, d.Movement().Radius) {
			t.Error("its walk grid has no room at the start, or room inside the house")
		}
	})
	t.Run("its areas and waypoints are its plan's, and a spot's area and safety are read from them", func(t *testing.T) {
		want := []gamedata.Area{
			{ID: "hamlet", Name: "Hamlet", Kind: gamedata.Town},
			{ID: "meadow", Name: "Meadow", Kind: gamedata.Hunting, MinLevel: 1, MaxLevel: 5, Boss: &gamedata.BossSpot{Mob: "slime_lord", At: gamedata.Point{X: 90, Z: 90}, Radius: 4}},
		}
		if !reflect.DeepEqual(m.Areas, want) {
			t.Errorf("areas %+v", m.Areas)
		}
		if want := []gamedata.Waypoint{{ID: "hamlet", Name: "Hamlet", At: gamedata.Point{X: 64, Z: 32}}}; !reflect.DeepEqual(m.Waypoints, want) {
			t.Errorf("waypoints %+v", m.Waypoints)
		}
		for _, c := range []struct {
			x, z float64
			area int
			town bool
		}{{64, 32, 0, true}, {64, 47.9, 0, true}, {64, 48, 1, false}, {100, 100, 1, false}, {8, 64, -1, false}, {120, 120, -1, false}} {
			if a, town := m.AreaAt(c.x, c.z), m.TownAt(c.x, c.z); a != c.area || town != c.town {
				t.Errorf("at (%g, %g) area %d, town %v; want %d, %v", c.x, c.z, a, town, c.area, c.town)
			}
		}
	})
	t.Run("a map has no bake and no areas: a town is safe everywhere, a hunting map nowhere", func(t *testing.T) {
		f, _ := d.Map("field")
		town, _ := d.Map("town")
		if f.Bake != "" || f.Areas != nil || f.Waypoints != nil || f.AreaAt(10, 10) != -1 {
			t.Errorf("field's bake %q, areas %v, waypoints %v", f.Bake, f.Areas, f.Waypoints)
		}
		if f.TownAt(10, 10) || !town.TownAt(10, 10) {
			t.Errorf("field safe %v, town safe %v", f.TownAt(10, 10), town.TownAt(10, 10))
		}
	})
}

func TestRegionRejections(t *testing.T) {
	const file = "regions/vale.yaml"
	tests := []struct {
		name    string
		file    string
		old     string
		replace string
		wantAll []string
	}{
		{"no name", file, "name: Vale", "name: ''", []string{file, "name"}},
		{"a key the plan doesn't have", file, "seed: 3", "seed: 3\nlevels: [1, 5]", []string{file, "levels"}},
		{"a plan the generator refuses", file, "cell: 2", "cell: 3", []string{file, "cell 3"}},

		// The areas.
		{"a square inside the edge in no area", file, "[[16, 16], [112, 16], [112, 48], [16, 48]]", "[[16, 16], [100, 16], [100, 48], [16, 48]]", []string{file, "square at [100 16]", "no area"}},
		{"two areas over one square", file, "[[16, 16], [112, 16], [112, 48], [16, 48]]", "[[16, 16], [112, 16], [112, 52], [16, 52]]", []string{file, "hamlet and meadow", "square at [16 48]"}},
		{"two areas of one id", file, "id: meadow", "id: hamlet", []string{file, "two areas are hamlet"}},
		{"an area without a name", file, "name: Meadow", "name: ''", []string{file, "area meadow", "name"}},
		{"a hunting area's levels out of order", file, "levels: [1, 5]", "levels: [5, 1]", []string{file, "area meadow", "1 <= lowest <= highest"}},
		{"a hunting area without a boss", file, ", kind: hunting,\n     boss: {at: [90, 90], radius: 4, mob: slime_lord}}", ", kind: hunting}", []string{file, "area meadow", "boss"}},
		{"a town with levels", file, "biome: meadow, kind: town}", "levels: [1, 2], biome: meadow, kind: town}", []string{file, "area hamlet", "town"}},
		{"a town with a boss", file, "biome: meadow, kind: town}", "biome: meadow, kind: town, boss: {at: [30, 30], radius: 4, mob: slime_lord}}", []string{file, "area hamlet", "town"}},
		{"a boss standing outside its area", file, "at: [90, 90], radius: 4", "at: [90, 40], radius: 4", []string{file, "area meadow", "slime_lord", "reaches area hamlet, outside area meadow"}},
		{"a boss circle across its area's edge", file, "at: [90, 90], radius: 4", "at: [90, 50], radius: 4", []string{file, "slime_lord", "[90 50], 4 m round, reaches area hamlet, outside area meadow"}},
		{"a boss circle into the ring", file, "at: [90, 90], radius: 4", "at: [110, 90], radius: 4", []string{file, "slime_lord", "reaches the ring, outside area meadow"}},
		{"a boss that isn't one", file, "radius: 4, mob: slime_lord", "radius: 4, mob: green_slime", []string{file, "area meadow", "green_slime", "boss"}},
		{"a boss below its area's top level", file, "levels: [1, 5]", "levels: [1, 6]", []string{file, "area meadow", "slime_lord", "level"}},
		{"a boss circle narrower than its body", file, "at: [90, 90], radius: 4", "at: [90, 90], radius: 1", []string{file, "area meadow", "slime_lord", "narrower"}},
		{"a boss in two areas", file, "biome: meadow, kind: town}", "levels: [1, 5], biome: meadow, kind: hunting, boss: {at: [30, 30], radius: 4, mob: slime_lord}}", []string{file, "area meadow", "slime_lord", "hamlet's already"}},
		{"a boss where it doesn't fit", file, "pieces:\n", "pieces:\n  - {house: {area: [87, 87, 6, 6], height: 4, model: house_small}}\n", []string{file, "boss slime_lord", "fits"}},

		// The camps.
		{"a camp of no area", file, "area: meadow, name: slimes", "area: marsh, name: slimes", []string{file, "camp \"slimes\"", "marsh"}},
		{"a camp in a town", file, "area: meadow, name: slimes, at: [40, 80]", "area: hamlet, name: slimes, at: [40, 30]", []string{file, "camp \"slimes\"", "town"}},
		{"a camp standing in another area", file, "name: slimes, at: [40, 80]", "name: slimes, at: [40, 30]", []string{file, "camp \"slimes\"", "reaches area hamlet, outside area meadow"}},
		{"a camp across the town's edge", file, "name: slimes, at: [40, 80]", "name: slimes, at: [40, 50]", []string{file, "camp \"slimes\"", "[40 50], 5 m round, reaches area hamlet, outside area meadow"}},
		{"a camp into the ring", file, "name: slimes, at: [40, 80]", "name: slimes, at: [20, 80]", []string{file, "camp \"slimes\"", "reaches the ring, outside area meadow"}},
		{"a camp's mob outside its area's levels", file, "levels: [1, 5]", "levels: [2, 5]", []string{file, "area meadow", "green_slime", "levels"}},
		{"a camp of a boss", file, "mob: green_slime", "mob: slime_lord", []string{file, "area meadow", "slimes", "boss"}},
		{"a camp of none", file, "max: 3", "max: 0", []string{file, "area meadow", "slimes", "max"}},
		{"a camp over a gate", file, "name: slimes, at: [40, 80]", "name: slimes, at: [28, 64]", []string{file, "slimes", "west", "portal"}},

		// The spots.
		{"a waypoint where a player doesn't fit", file, "at: [64, 32]}", "at: [83, 27]}", []string{file, "waypoint hamlet", "fits"}},
		{"two waypoints of one id", file, "waypoints:\n", "waypoints:\n  - {id: hamlet, name: Again, at: [40, 32]}\n", []string{file, "two waypoints are hamlet"}},
		{"a town without a waypoint", file, "at: [64, 32]}", "at: [64, 80]}", []string{file, "town hamlet holds no waypoint"}},
		{"a waypoint in the ring", file, "at: [64, 32]}", "at: [8, 64]}", []string{file, "waypoint hamlet at [8 64] stands in the ring"}},
		{"the start in the ring", file, "start: {at: [64, 28]", "start: {at: [64, 4]", []string{file, "the start at [64 4] stands in the ring"}},
		{"an npc in the ring", file, "start:", "npcs:\n  - {id: guide, at: [120, 64], yaw: 0}\nstart:", []string{file, "NPC guide at [120 64] stands in the ring"}},
		{"a start where a player doesn't fit", file, "start: {at: [64, 28]", "start: {at: [83, 27]", []string{file, "player_spawn", "fits"}},
		{"an npc of no npcs.yaml", file, "start:", "npcs:\n  - {id: stranger, at: [70, 40], yaw: 0}\nstart:", []string{file, "stranger"}},
		{"an npc already on a map", file, "start:", "npcs:\n  - {id: guide, at: [70, 40], yaw: 0}\nstart:", []string{"regions/vale.yaml", "guide", "already stands on"}},

		// The gates.
		{"two gates of one name", file, "gates:\n", "gates:\n  - {name: west, at: [24, 90], radius: 1.6, to: {region: town, gate: to_vale}}\n", []string{file, "duplicate", "west"}},
		{"a gate to no region", file, "to: {region: town, gate: to_vale}", "to: {region: marsh, gate: to_vale}", []string{file, "west", "marsh"}},
		{"a gate to a portal leading elsewhere", "maps/town.yaml", "to: {map: vale, portal: west}", "to: {map: field, portal: from_town}", []string{file, "gate \"west\"", "not back"}},
		{"a portal to a gate leading elsewhere", "maps/field.yaml", "portals:\n", "portals:\n  - {name: to_vale, at: [2, 3], radius: 1.2, to: {map: vale, portal: west}}\n", []string{"maps/field.yaml", "portal \"to_vale\" leads to vale's \"west\", which leads to town's \"to_vale\", not back"}},
		{"a gate leading to itself", file, "to: {region: town, gate: to_vale}", "to: {region: vale, gate: west}", []string{file, "portal \"west\" leads to itself"}},
		{"a gate in the ring", file, "name: west, at: [24, 64]", "name: west, at: [12, 64]", []string{file, "gate west at [12 64] stands in the ring"}},

		// Across files.
		{"a place for a region", "world.yaml", "  field: [60, 50]\n", "  field: [60, 50]\n  vale: [10, 10]\n", []string{"world.yaml", "vale", "region"}},
		{"a region one interest block holds", "world.yaml", "  behind: 1\n", "  behind: 3\n", []string{file, "block", "whole"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := regioned(t)
			if !strings.Contains(files[tt.file], tt.old) {
				t.Fatalf("%s has no %q", tt.file, tt.old)
			}
			files[tt.file] = strings.Replace(files[tt.file], tt.old, tt.replace, 1)
			_, err := load.Dir(write(t, files))
			if err == nil {
				t.Fatal("loaded")
			}
			for _, w := range tt.wantAll {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
		})
	}
	t.Run("a region of a map's id", func(t *testing.T) {
		files := regioned(t)
		files["regions/field.yaml"] = vale
		if _, err := load.Dir(write(t, files)); err == nil || !strings.Contains(err.Error(), "regions/field.yaml") || !strings.Contains(err.Error(), "a map has the id field") {
			t.Fatalf("error %v", err)
		}
	})
	t.Run("a region without the kit", func(t *testing.T) {
		files := regioned(t)
		delete(files, "kit.yaml")
		if _, err := load.Dir(write(t, files)); err == nil || !strings.Contains(err.Error(), "kit.yaml") {
			t.Fatalf("error %v", err)
		}
	})
}
