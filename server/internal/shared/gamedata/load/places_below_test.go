package load_test

import (
	"strings"
	"testing"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
)

// hollow is a cave below Vale (underground-and-underwater.md section 3): one hall carved from its rock, hunted all through, its gate up to Vale's and a waypoint by it.
const hollow = `
name: Hollow
under: vale
size: [128, 128]
cell: 1
seed: 5
look: {sky: cave}
ground: {noise: {scale: 60, amplitude: 1, octaves: 2}}
edge: {width: 16, height: [2, 4], biome: rock}
colour: {slope: 0.25, high: 100}
rock: {height: 4.5, rough: 1}
features:
  - {hall: {area: [[24, 40], [104, 40], [104, 96], [24, 96]]}}
areas:
  - {id: hollow, name: The Hollow, area: [[16, 16], [112, 16], [112, 112], [16, 112]], levels: [1, 5], biome: cave, kind: hunting,
     boss: {at: [80, 70], radius: 4, mob: slime_lord}}
camps:
  - {area: hollow, name: slimes, at: [64, 80], radius: 5, mob: green_slime, max: 3, respawn: 5s}
waypoints:
  - {id: hollow_mouth, name: Hollow Mouth, at: [40, 64]}
gates:
  - {name: up, at: [30, 64], radius: 1.6, to: {region: vale, gate: down}}
start: {at: [34, 64], yaw: 90}
biomes:
  cave:
    colours: {flat: cave_floor, slope: cave_floor, steep: cave_rock, high: cave_rock}
  rock:
    colours: {flat: cave_rock, slope: cave_rock, steep: cave_rock, high: cave_rock}
`

// deep is the fixture with Vale and Hollow below it, and Vale's gate down.
func deep(t *testing.T) map[string]string {
	t.Helper()
	files := regioned(t)
	files["regions/hollow.yaml"] = hollow
	files["regions/vale.yaml"] = strings.Replace(vale, "gates:\n", "gates:\n  - {name: down, at: [100, 64], radius: 1.6, to: {region: hollow, gate: up}}\n", 1)
	return files
}

// A place below loads as a region that names the region above it; a region on the land names none.
func TestAPlaceBelowLoadsUnderItsRegion(t *testing.T) {
	d, err := load.Dir(write(t, deep(t)))
	if err != nil {
		t.Fatal(err)
	}
	h, _ := d.Map("hollow")
	v, _ := d.Map("vale")
	if h.Kind != gamedata.Region || h.Under != "vale" || v.Under != "" {
		t.Fatalf("hollow a %s under %q, vale under %q; want a region under vale, and vale under none", h.Kind, h.Under, v.Under)
	}
}

// A place below is refused unless it's under a region on the land, its gates all lead up to that region, each with a waypoint near, and it has no town.
func TestPlaceBelowRejections(t *testing.T) {
	const file = "regions/hollow.yaml"
	type edit struct{ file, old, replace string }
	for _, tt := range []struct {
		name    string
		edits   []edit
		wantAll []string
	}{
		{"under no region", []edit{{file, "under: vale", "under: marsh"}}, []string{file, "under marsh, which is no region"}},
		{"under a place below", []edit{
			{"regions/abyss.yaml", "under: vale", "under: hollow"},
			{"regions/abyss.yaml", "id: hollow_mouth", "id: abyss_mouth"},
		}, []string{"regions/abyss.yaml", "under hollow, which is itself below vale"}},
		{"no gate up", []edit{
			{file, "  - {name: up, at: [30, 64], radius: 1.6, to: {region: vale, gate: down}}\n", ""},
			{"regions/vale.yaml", "  - {name: down, at: [100, 64], radius: 1.6, to: {region: hollow, gate: up}}\n", ""},
		}, []string{file, "needs a gate up to vale"}},
		{"a gate leading elsewhere", []edit{{file, "to: {region: vale, gate: down}", "to: {region: town, gate: to_vale}"}}, []string{file, "gate up leads to town", "lead up to vale"}},
		{"a gate with no waypoint near", []edit{{file, "at: [40, 64]}", "at: [60, 44]}"}}, []string{file, "gate up has no waypoint within 20 m"}}, // clear of the boss and camp, as a stone keeps
		{"a camp by its waypoint's stone", []edit{{file, "name: slimes, at: [64, 80]", "name: slimes, at: [56, 70]"}}, []string{file, `camp "slimes"`, "within 12.09 m of waypoint hollow_mouth's stone at [40 64]", "keeps 20 m"}},
		{"a town below", []edit{
			{file, "levels: [1, 5], biome: cave, kind: hunting,\n     boss: {at: [80, 70], radius: 4, mob: slime_lord}}", "biome: cave, kind: town}"},
			{file, "camps:\n  - {area: hollow, name: slimes, at: [64, 80], radius: 5, mob: green_slime, max: 3, respawn: 5s}\n", ""},
		}, []string{file, "it has no town"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := deep(t)
			if strings.HasPrefix(tt.edits[0].file, "regions/abyss") {
				files["regions/abyss.yaml"] = hollow
			}
			for _, e := range tt.edits {
				if !strings.Contains(files[e.file], e.old) {
					t.Fatalf("%s has no %q", e.file, e.old)
				}
				files[e.file] = strings.Replace(files[e.file], e.old, e.replace, 1)
			}
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
}
