package mobs_test

import (
	"math"
	"math/rand/v2"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"finga/internal/application/world/mobs"
	"finga/internal/application/world/worldtest"
	"finga/internal/model/world"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// withCell is the shipped data with interest cells cell metres wide, and as many rows behind a player as cover 32 m, as the loader asks; without its regions, which one block may hold, as the loader refuses.
func withCell(t *testing.T, cell string) *gamedata.Data {
	t.Helper()
	m, err := strconv.ParseFloat(cell, 64)
	if err != nil {
		t.Fatal(err)
	}
	behind := strconv.Itoa(int(math.Ceil(32 / m)))
	return worldtest.LoadDataWith(t, worldtest.WithoutRegions(t, map[string]func(string) string{"world.yaml": func(s string) string {
		return strings.NewReplacer("  cell: 32\n", "  cell: "+cell+"\n", "  behind: 1\n", "  behind: "+behind+"\n").Replace(s)
	}}))
}

// The mobs a scene draws from: every kind of attack, and a boss's slam and wide body.
var sceneMobs = []string{"green_slime", "orange_mushroom", "wild_boar", "cave_bat", "mossback_snail", "king_slime", "vampire_bat"}

// large-regions.md section 5: an arena reading its bodies by interest cell runs as one whose single cell holds the whole map, which reads every body. Cells of 2 to 32 m, so a query too short by any of a body's width shows, and each kind walks cells in some and all bodies in others; spawns across cell lines, players among them who wander, jump across the map and strike at the mobs, and ticks with nobody there; compared tick by tick: every view, attack begun, landing, damage over time, change and blow.
func TestAnArenaInCellsRunsAsOneReadingEveryBody(t *testing.T) {
	inCells := map[float64]*gamedata.Data{2: withCell(t, "2"), 4: withCell(t, "4"), 8: withCell(t, "8"), 32: worldtest.LoadData(t)}
	oneCell := withCell(t, "100000")
	for c, d := range inCells {
		if d.Interest().Cell != c || oneCell.Interest().Cell != 100000 {
			t.Fatalf("interest cells %v and %v; want %v and 100000", d.Interest().Cell, oneCell.Interest().Cell, c)
		}
	}
	grounds := map[float64]sim.Geometry{}
	grids := map[float64]*nav.Grid{}
	for _, size := range []float64{40, 72, 130} { // flat ground with a wall every 13 m, which mobs stall at, its grid built once
		var walls []sim.Box
		for x := 7.0; x < size-4; x += 13 {
			for z := 9.0; z < size-4; z += 13 {
				walls = append(walls, sim.Box{X: x, Z: z, HalfW: 1, HalfD: 1.5, UX: 1, Top: 3})
			}
		}
		grounds[size] = sim.Indexed(sim.Geometry{SizeX: size, SizeZ: size, Terrain: sim.NewTerrain(size, 2, 2, []float64{0, 0, 0, 0}, 0.7), Boxes: walls, Cylinders: []sim.Cylinder{}})
		grids[size] = nav.New(grounds[size], oneCell.Movement().Step)
	}
	rapid.Check(t, func(t *rapid.T) {
		cell := rapid.SampledFrom([]float64{2, 4, 8, 32}).Draw(t, "cell")
		size := rapid.SampledFrom([]float64{40, 72, 130}).Draw(t, "size")
		across := func(label string) float64 { // by a cell line
			line := cell * float64(rapid.IntRange(1, max(1, int(size/cell)-1)).Draw(t, label+" line"))
			return min(max(line+rapid.Float64Range(-3, 3).Draw(t, label), 3), size-3)
		}
		var spawns []gamedata.Spawn
		for range rapid.IntRange(1, 6).Draw(t, "spawns") {
			at := gamedata.Point{X: across("spawn x"), Z: across("spawn z")}
			spawns = append(spawns, gamedata.Spawn{Mob: rapid.SampledFrom(sceneMobs).Draw(t, "mob"), Max: rapid.IntRange(1, 5).Draw(t, "max"), Respawn: tick * 20, At: at, Radius: rapid.Float64Range(1, 5).Draw(t, "spawn radius")})
		}
		grid := grids[size]
		m := &gamedata.Map{ID: "meadow", Kind: gamedata.Hunting, Geometry: grounds[size], Spawns: spawns, Grid: func() *nav.Grid { return grid }}
		seed := rapid.Uint64().Draw(t, "seed")
		a := mobs.NewArena(mobs.Deps{Map: m, Data: inCells[cell], Roll: rand.New(rand.NewPCG(seed, 1))}, mobs.Tuning{TickInterval: tick})
		b := mobs.NewArena(mobs.Deps{Map: m, Data: oneCell, Roll: rand.New(rand.NewPCG(seed, 1))}, mobs.Tuning{TickInterval: tick})
		players := make([]mobs.Body, rapid.IntRange(0, 6).Draw(t, "players"))
		for i := range players { // each among a spawn's mobs
			at := spawns[rapid.IntRange(0, len(spawns)-1).Draw(t, "near spawn")].At
			players[i] = mobs.Body{ID: kernel.NewCharacterID(), X: min(max(at.X+rapid.Float64Range(-4, 4).Draw(t, "off x"), 1), size-1), Z: min(max(at.Z+rapid.Float64Range(-4, 4).Draw(t, "off z"), 1), size-1), Radius: playerRadius, Height: playerHeight}
		}
		same := func(n uint64, what string, x, y any) {
			if !reflect.DeepEqual(x, y) {
				t.Fatalf("tick %d, %s: %+v in %v m cells, %+v in one cell", n, what, x, cell, y)
			}
		}
		for n := uint64(1); n <= 60; n++ {
			for i := range players {
				p := &players[i]
				if rapid.IntRange(0, 29).Draw(t, "jump") == 0 { // across a cell or several
					p.X, p.Z = rapid.Float64Range(1, size-1).Draw(t, "to x"), rapid.Float64Range(1, size-1).Draw(t, "to z")
				} else {
					p.X = min(max(p.X+rapid.Float64Range(-0.3, 0.3).Draw(t, "dx"), 1), size-1)
					p.Z = min(max(p.Z+rapid.Float64Range(-0.3, 0.3).Draw(t, "dz"), 1), size-1)
				}
				p.Y = rapid.SampledFrom([]float64{0, 0, 0, 0, 1.2}).Draw(t, "y")
			}
			if len(players) == 0 || rapid.IntRange(0, 19).Draw(t, "rest") == 0 {
				a.Rest(n)
				b.Rest(n)
			} else {
				a.Step(n, players, nil)
				b.Step(n, players, nil)
			}
			same(n, "views", a.Views(), b.Views())
			same(n, "attacks begun", a.Started(), b.Started())
			same(n, "landings", a.Landings(), b.Landings())
			same(n, "damage over time", a.Dots(), b.Dots())
			if views := a.Views(); len(players) > 0 && len(views) > 0 && rapid.IntRange(0, 3).Draw(t, "strike") == 0 {
				p := players[rapid.IntRange(0, len(players)-1).Draw(t, "striker")]
				at := views[rapid.IntRange(0, len(views)-1).Draw(t, "at mob")]
				yaw := math.Atan2(at.X-p.X, at.Z-p.Z) + rapid.Float64Range(-0.3, 0.3).Draw(t, "off aim")
				s := mobs.Strike{By: p.ID, Most: rapid.IntRange(1, 4).Draw(t, "most"), Power: strong, Aim: world.Aim{X: p.X, Y: p.Y, Z: p.Z, Yaw: yaw}}
				s.Shape = rapid.SampledFrom([]gamedata.Shape{
					{Kind: gamedata.ArcShape, Radius: 2.375, Angle: 120, Height: 1.75},
					{Kind: gamedata.ShotShape, Range: 13.125, Width: 0.5},
					{Kind: gamedata.AreaShape, Radius: 3},
				}).Draw(t, "shape")
				if rapid.Bool().Draw(t, "aim at") {
					s.Target = at.ID
				}
				same(n, "blows", a.Strike(s), b.Strike(s))
			}
			ac, ar := a.Changes()
			bc, br := b.Changes()
			same(n, "changes", ac, bc)
			same(n, "removed", ar, br)
		}
	})
}
