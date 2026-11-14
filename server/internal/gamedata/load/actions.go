package load

import (
	"errors"
	"fmt"
	"time"

	"finga/internal/gamedata"
)

const actionsFile = "actions.yaml"

type actionsYAML struct {
	Cancel *float64 `yaml:"cancel"`
	Ready  *float64 `yaml:"ready"`
	Energy *struct {
		Max       int `yaml:"max"`
		PerAttack int `yaml:"per_attack"`
	} `yaml:"energy"`
}

// loadActions reads actions.yaml before the classes and skills, whose actions take its cancel and ready points unless they name their own and cost its energy.
func (l *loader) loadActions(dir string) error {
	var y actionsYAML
	if err := readYAML(dir, actionsFile, &y); err != nil {
		return err
	}
	switch {
	case y.Cancel == nil || y.Ready == nil || y.Energy == nil:
		return fmt.Errorf("%s: needs cancel, ready and energy", actionsFile)
	case !isShare(*y.Cancel) || !isShare(*y.Ready):
		return fmt.Errorf("%s: cancel and ready must be shares of a length, from 0 to 1", actionsFile)
	case y.Energy.Max < 1:
		return fmt.Errorf("%s: energy needs a max of at least 1", actionsFile)
	case y.Energy.PerAttack < 1 || y.Energy.PerAttack > y.Energy.Max:
		return fmt.Errorf("%s: energy's per_attack must be from 1 to its max", actionsFile)
	}
	l.shares = gamedata.Timing{Cancel: *y.Cancel, Ready: *y.Ready}
	l.energy = gamedata.Energy{Max: y.Energy.Max, PerAttack: y.Energy.PerAttack}
	return nil
}

// timingYAML is an action's length and, where it names them, its hit, cancel point and ready point.
type timingYAML struct {
	Length time.Duration `yaml:"length"`
	Hit    *float64      `yaml:"hit"`
	Cancel *float64      `yaml:"cancel"`
	Ready  *float64      `yaml:"ready"`
}

// over is t with what raw names.
func (raw timingYAML) over(t gamedata.Timing) gamedata.Timing {
	if raw.Length != 0 {
		t.Length = raw.Length
	}
	if raw.Hit != nil {
		t.Hit = *raw.Hit
	}
	if raw.Cancel != nil {
		t.Cancel = *raw.Cancel
	}
	if raw.Ready != nil {
		t.Ready = *raw.Ready
	}
	return t
}

func (raw timingYAML) named() bool {
	return raw.Length != 0 || raw.Hit != nil || raw.Cancel != nil || raw.Ready != nil
}

// checkTiming refuses a length that isn't positive, a share outside 0 to 1, and a hit after the cancel or ready point; the world refuses a length under its tick.
func checkTiming(t gamedata.Timing) error {
	switch {
	case t.Length <= 0:
		return errors.New("length must be positive")
	case !isShare(t.Hit) || !isShare(t.Cancel) || !isShare(t.Ready):
		return errors.New("hit, cancel and ready are shares of the length, from 0 to 1")
	case t.Hit > t.Cancel:
		return errors.New("the hit must land by the cancel point")
	case t.Hit > t.Ready:
		return errors.New("the hit must land by the ready point")
	}
	return nil
}

func isShare(v float64) bool { return v >= 0 && v <= 1 }
