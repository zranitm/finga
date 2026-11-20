package nav_test

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"gopkg.in/yaml.v3"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/gamedatatest"
	"finga/internal/shared/mapfile"
	"finga/internal/shared/nav"
	"finga/internal/shared/regiongen"
	"finga/internal/shared/sim"
)

var update = flag.Bool("update", false, "rewrite testdata/nav/grid.sha256 from the shipped maps")

const (
	// jumpMargin keeps the reach check's jumps short of the sim's peak, so a route never hangs on a frame-perfect jump.
	jumpMargin = 0.1
)

// shippedParams are data/movement.yaml's.
func shippedParams() (sim.Params, error) {
	raw, err := os.ReadFile("../../../data/movement.yaml")
	if err != nil {
		return sim.Params{}, err
	}
	var m struct {
		Dt           float64 `yaml:"dt"`
		WalkSpeed    float64 `yaml:"walk_speed"`
		JumpVelocity float64 `yaml:"jump_velocity"`
		Gravity      float64 `yaml:"gravity"`
		MaxFallSpeed float64 `yaml:"max_fall_speed"`
		Radius       float64 `yaml:"radius"`
		Height       float64 `yaml:"height"`
		Step         float64 `yaml:"step"`
		MaxSlope     float64 `yaml:"max_slope"`
		Snap         float64 `yaml:"snap"`
	}
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return sim.Params{}, err
	}
	return sim.Params{Dt: m.Dt, WalkSpeed: m.WalkSpeed, JumpVelocity: m.JumpVelocity, Gravity: m.Gravity, MaxFallSpeed: m.MaxFallSpeed,
		Radius: m.Radius, Height: m.Height, Step: m.Step, MaxSlope: m.MaxSlope, Snap: m.Snap}, nil
}

type shippedData struct {
	params sim.Params
	maps   []*mapfile.Map
}

// loadShipped builds data/maps's maps once per test binary, so each grid is built once for every test that reads it.
var loadShipped = sync.OnceValues(func() (shippedData, error) {
	p, err := shippedParams()
	if err != nil {
		return shippedData{}, err
	}
	files, err := filepath.Glob("../../../data/maps/*.yaml")
	if err != nil {
		return shippedData{}, err
	}
	if len(files) == 0 {
		return shippedData{}, errors.New("no maps in data/maps")
	}
	out := shippedData{params: p}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			return shippedData{}, err
		}
		var f mapfile.File
		if err := yaml.Unmarshal(raw, &f); err != nil {
			return shippedData{}, err
		}
		m, err := mapfile.Build(strings.TrimSuffix(filepath.Base(path), ".yaml"), f, p)
		if err != nil {
			return shippedData{}, err
		}
		out.maps = append(out.maps, m)
	}
	return out, nil
})

func shipped(t *testing.T) shippedData {
	t.Helper()
	d, err := loadShipped()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// spot is a place the reach check starts from, or one it reaches to within some distance.
type spot struct {
	what   string
	at     [2]float64
	within float64
}

// crossed is the reach check on g: a player gets from each start to every target, jumping rise at most.
func crossed(t *testing.T, g *nav.Grid, radius, rise float64, starts, targets []spot) {
	t.Helper()
	if len(targets) == 0 {
		t.Fatal("nothing to reach, so nothing checked")
	}
	for _, s := range starts {
		got := g.Reach([]nav.Point{mapfile.Point(s.at)}, radius, rise)
		for _, tg := range targets {
			if !got.Near(mapfile.Point(tg.at), tg.within) {
				t.Errorf("from %s, a player can't reach %s", s.what, tg.what)
			}
		}
	}
}

// fastRise is how high a jump lifts the feet at the fastest a player moves, less jumpMargin: the move_speed limit caps gear, passives and buffs too, and the jump peaks lowest at the fastest speed, so a route on it holds at all.
func fastRise(data *gamedata.Data, p sim.Params) float64 {
	return nav.JumpRise(p.Faster(sim.SpeedFactor(data.StatRules().Curves.MoveSpeed.Limit))) - jumpMargin
}

// The reach check (docs/tech/3d-world.md section 4): on every shipped map, players get from each start to every target at every speed.
func TestEveryShippedMapCanBeCrossed(t *testing.T) {
	data := gamedatatest.Shipped(t)
	d := shipped(t)
	p := d.params
	rise := fastRise(data, p)
	for _, m := range d.maps {
		t.Run(m.ID, func(t *testing.T) {
			starts := []spot{{what: "the player spawn", at: m.PlayerSpawn.At}}
			var targets []spot
			if m.Kind == "pvp" { // a match's fighters start on its sides and meet with the prep wall down
				starts = nil
				for k, side := range m.Sides {
					for i, at := range side {
						what := fmt.Sprintf("side %d's spot %d", k+1, i+1)
						starts = append(starts, spot{what: what, at: at.At})
						targets = append(targets, spot{what, at.At, p.Radius})
					}
				}
			}
			for _, pt := range m.Portals {
				starts = append(starts, spot{what: "portal " + pt.Name, at: pt.At})
				targets = append(targets, spot{"portal " + pt.Name, pt.At, pt.Radius})
			}
			for _, s := range m.Spawns {
				targets = append(targets, spot{"spawn " + s.Name, s.At, s.Radius})
			}
			if b := m.Boss; b != nil {
				targets = append(targets, spot{"boss " + b.Mob, b.At, b.Radius})
			}
			for _, n := range m.NPCs {
				targets = append(targets, spot{"npc " + n.ID, n.At, data.TalkReach()})
			}
			crossed(t, m.Grid(), p.Radius, rise, starts, targets)
		})
	}
}

// The reach check on every shipped region (large-regions.md section 3): from its start, each gate and beside each waypoint, where teleports land and the dead get up, players get to every waypoint, gate, NPC, camp and area boss at every speed, on the walk grid the server builds.
func TestEveryShippedRegionCanBeCrossed(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := shipped(t).params
	rise := fastRise(data, p)
	regions := shippedRegions(t)
	if len(regions) == 0 {
		t.Fatal("no regions in data/regions")
	}
	for id, plan := range regions {
		t.Run(id, func(t *testing.T) {
			m, ok := data.Map(id)
			if !ok {
				t.Fatalf("no region %s in the data", id)
			}
			starts := []spot{{what: "the start", at: plan.Start.At}}
			var targets []spot
			for _, g := range plan.Gates {
				starts = append(starts, spot{what: "gate " + g.Name, at: g.At})
				targets = append(targets, spot{"gate " + g.Name, g.At, g.Radius})
			}
			for _, w := range m.Waypoints {
				starts = append(starts, spot{what: "beside waypoint " + w.ID, at: [2]float64{w.Arrive.X, w.Arrive.Z}})
				targets = append(targets, spot{"waypoint " + w.ID, [2]float64{w.At.X, w.At.Z}, p.Radius})
			}
			for _, n := range plan.NPCs {
				targets = append(targets, spot{"npc " + n.ID, n.At, data.TalkReach()})
			}
			for _, c := range plan.Camps {
				targets = append(targets, spot{"camp " + c.Name, c.At, c.Radius})
			}
			for _, a := range plan.Areas {
				if b := a.Boss; b != nil {
					targets = append(targets, spot{"area " + a.ID + "'s boss", b.At, b.Radius})
				}
			}
			crossed(t, m.Grid(), p.Radius, rise, starts, targets)
		})
	}
}

// shippedRegions are data/regions' plans by id.
func shippedRegions(t *testing.T) map[string]regiongen.Plan {
	t.Helper()
	files, err := filepath.Glob("../../../data/regions/*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]regiongen.Plan{}
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := regiongen.Decode(raw)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		out[strings.TrimSuffix(filepath.Base(path), ".yaml")] = plan
	}
	return out
}

// awkward's solids sit off the half-metre grid with axes that aren't 0 or ±1, so every product rounds and a fused one shows.
var awkward = flat(14, 14,
	[]sim.Box{
		{X: 3.3, Z: 4.7, HalfW: 1.1, HalfD: 0.7, UX: 0.6, UZ: 0.8, Top: 2.3},
		{X: 9.1, Z: 8.9, HalfW: 0.9, HalfD: 1.3, UX: 0.28, UZ: -0.96, Top: 1.7},
		{X: 6.6, Z: 11.3, HalfW: 1.2, HalfD: 0.4, UX: 0.96, UZ: 0.28, Top: 0.2},
	},
	[]sim.Cylinder{{X: 10.7, Z: 3.1, Radius: 0.9, Top: 4}, {X: 2.9, Z: 11.3, Radius: 0.6, Top: 0.25}})

// Every grid's clearances and flags, and so its walks, are the same bits on every CPU, as the loader's spot verdicts must be; amd64 and arm64 CI check.
func TestShippedGridBits(t *testing.T) {
	const golden = "../../../testdata/nav/grid.sha256"
	d := shipped(t)
	hash := func(id string, n *nav.Grid) string {
		clear, flags := n.Cells()
		var b []byte
		for _, v := range clear {
			b = binary.LittleEndian.AppendUint16(b, v)
		}
		b = append(b, flags...)
		return fmt.Sprintf("%s %x", id, sha256.Sum256(b))
	}
	lines := []string{hash("awkward", nav.New(awkward, d.params.Step))}
	for _, m := range d.maps {
		lines = append(lines, hash(m.ID, m.Grid()))
	}
	got := strings.Join(lines, "\n") + "\n"
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
		t.Errorf("grid bits differ (go test ./internal/shared/nav -run TestShippedGridBits -update after a map edit)\ngot:\n%swant:\n%s", got, want)
	}
}
