package load

import (
	"fmt"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/nav"
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
	EnemyHeight  float64 `yaml:"enemy_height"`
	Step         float64 `yaml:"step"`
	MaxSlope     float64 `yaml:"max_slope"`
	Snap         float64 `yaml:"snap"`
	Sprint       struct {
		After float64 `yaml:"after"`
		Speed float64 `yaml:"speed"`
	} `yaml:"sprint"`
}

// Movement is dir's movement.yaml checked as Dir checks it, for a tool that needs the sim's rules and nothing else.
func Movement(dir string) (sim.Params, error) {
	var l loader
	if err := l.loadMovement(dir); err != nil {
		return sim.Params{}, fmt.Errorf("gamedata: %w", err)
	}
	return l.movement, nil
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
		{"max_fall_speed", m.MaxFallSpeed}, {"radius", m.Radius}, {"height", m.Height}, {"enemy_height", m.EnemyHeight}, {"step", m.Step},
		{"max_slope", m.MaxSlope}, {"snap", m.Snap}, {"sprint.after", m.Sprint.After},
	} {
		if !positive(v.value) {
			return fmt.Errorf("%s: %s must be positive", movementFile, v.name)
		}
	}
	if err := nav.CheckRadius(m.Radius); err != nil {
		return fmt.Errorf("%s: radius: %w", movementFile, err)
	}
	if err := nav.CheckRadius(m.Radius + gamedata.BotPathMargin); err != nil {
		return fmt.Errorf("%s: a bot's path radius, the radius and its margin: %w", movementFile, err)
	}
	if m.EnemyHeight > m.Height {
		return fmt.Errorf("%s: enemy_height must be at most height", movementFile)
	}
	if !(m.Sprint.Speed > 1) {
		return fmt.Errorf("%s: sprint.speed must be over 1", movementFile)
	}
	l.movement = sim.Params{
		Dt: m.Dt, WalkSpeed: m.WalkSpeed, JumpVelocity: m.JumpVelocity, Gravity: m.Gravity, MaxFallSpeed: m.MaxFallSpeed,
		Radius: m.Radius, Height: m.Height, EnemyHeight: m.EnemyHeight, Step: m.Step, MaxSlope: m.MaxSlope, Snap: m.Snap,
		Sprint: sim.Sprint{After: m.Sprint.After, Speed: m.Sprint.Speed},
	}
	return nil
}
