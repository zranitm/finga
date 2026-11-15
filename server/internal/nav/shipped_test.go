package nav_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"finga/internal/gamedata/gamedatatest"
	"finga/internal/maps3d"
	"finga/internal/nav"
	"finga/internal/sim"
	"finga/internal/sim3d"
)

const (
	pxPerMetre = 32 // npcs.yaml's talk_reach is still in px

	// jumpMargin keeps the reach check's jumps short of the sim's peak, so a route never hangs on a frame-perfect jump.
	jumpMargin = 0.1
)

// shippedParams are data/movement3d.yaml's.
func shippedParams(t *testing.T) sim3d.Params {
	t.Helper()
	raw, err := os.ReadFile("../../data/movement3d.yaml")
	if err != nil {
		t.Fatal(err)
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
		t.Fatal(err)
	}
	return sim3d.Params{Dt: m.Dt, WalkSpeed: m.WalkSpeed, JumpVelocity: m.JumpVelocity, Gravity: m.Gravity, MaxFallSpeed: m.MaxFallSpeed,
		Radius: m.Radius, Height: m.Height, Step: m.Step, MaxSlope: m.MaxSlope, Snap: m.Snap}
}

// The reach check (docs/tech/3d-world.md section 4): on every shipped map, from the player spawn and from each portal a player arrives
// at, every portal, spawn, the boss's circle, and every NPC within talking reach can be got to, at every move speed.
func TestEveryShippedMapCanBeCrossed(t *testing.T) {
	data := gamedatatest.Shipped(t)
	p := shippedParams(t)
	// The move_speed curve's limit caps gear, passives and buffs too (progression.DeriveStats), so it's the fastest a player moves.
	// The sim's jump peaks lowest at the fastest speed, so a check at that speed holds at every speed.
	fastest := sim.SpeedFactor(data.StatRules().Curves.MoveSpeed.Limit)
	rise := nav.JumpRise(p.Faster(fastest)) - jumpMargin
	files, err := filepath.Glob("../../data/maps3d/*.yaml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no maps: %v", err)
	}
	for _, path := range files {
		id := strings.TrimSuffix(filepath.Base(path), ".yaml")
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var f maps3d.File
			if err := yaml.Unmarshal(raw, &f); err != nil {
				t.Fatal(err)
			}
			m, err := maps3d.Build(id, f, p)
			if err != nil {
				t.Fatal(err)
			}
			type spot struct {
				what   string
				at     [2]float64
				within float64
			}
			starts := []spot{{what: "the player spawn", at: m.PlayerSpawn.At}}
			var targets []spot
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
				targets = append(targets, spot{"npc " + n.ID, n.At, data.TalkReach() / pxPerMetre})
			}
			for _, s := range starts {
				got := m.Grid().Reach([]nav.Point{maps3d.Point(s.at)}, p.Radius, rise)
				for _, tg := range targets {
					if !got.Near(maps3d.Point(tg.at), tg.within) {
						t.Errorf("from %s, a player can't reach %s", s.what, tg.what)
					}
				}
			}
		})
	}
}
