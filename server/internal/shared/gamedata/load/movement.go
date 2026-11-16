package load

import (
	"fmt"

	"finga/internal/shared/sim"
)

const movementFile = "movement.yaml"

type movementYAML struct {
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

func (l *loader) loadMovement(dir string) error {
	var m movementYAML
	if err := readYAML(dir, movementFile, &m); err != nil {
		return err
	}
	for _, v := range []struct {
		name  string
		value float64
	}{
		{"dt", m.Dt}, {"walk_speed", m.WalkSpeed}, {"jump_velocity", m.JumpVelocity}, {"gravity", m.Gravity},
		{"max_fall_speed", m.MaxFallSpeed}, {"radius", m.Radius}, {"height", m.Height}, {"step", m.Step},
		{"max_slope", m.MaxSlope}, {"snap", m.Snap},
	} {
		if !positive(v.value) {
			return fmt.Errorf("%s: %s must be positive", movementFile, v.name)
		}
	}
	l.movement = sim.Params{
		Dt: m.Dt, WalkSpeed: m.WalkSpeed, JumpVelocity: m.JumpVelocity, Gravity: m.Gravity, MaxFallSpeed: m.MaxFallSpeed,
		Radius: m.Radius, Height: m.Height, Step: m.Step, MaxSlope: m.MaxSlope, Snap: m.Snap,
	}
	return nil
}
