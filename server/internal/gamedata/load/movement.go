package load

import (
	"fmt"

	"finga/internal/sim"
)

const movementFile = "movement.yaml"

type movementYAML struct {
	Dt           float64 `yaml:"dt"`
	WalkSpeed    float64 `yaml:"walk_speed"`
	JumpVelocity float64 `yaml:"jump_velocity"`
	Gravity      float64 `yaml:"gravity"`
	MaxFallSpeed float64 `yaml:"max_fall_speed"`
	ClimbSpeed   float64 `yaml:"climb_speed"`
	DropThrough  float64 `yaml:"drop_through"`
	Width        float64 `yaml:"width"`
	Height       float64 `yaml:"height"`
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
		{"max_fall_speed", m.MaxFallSpeed}, {"climb_speed", m.ClimbSpeed}, {"drop_through", m.DropThrough},
		{"width", m.Width}, {"height", m.Height},
	} {
		if !positive(v.value) {
			return fmt.Errorf("%s: %s must be positive", movementFile, v.name)
		}
	}
	l.movement = sim.Params{
		Dt: m.Dt, WalkSpeed: m.WalkSpeed, JumpVelocity: m.JumpVelocity, Gravity: m.Gravity, MaxFallSpeed: m.MaxFallSpeed,
		ClimbSpeed: m.ClimbSpeed, DropThrough: m.DropThrough, Width: m.Width, Height: m.Height,
	}
	return nil
}
