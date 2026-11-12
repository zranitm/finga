package load

import (
	"errors"
	"fmt"
	"time"

	"finga/internal/gamedata"
)

const actionsFile = "actions.yaml"

// maxEarlyPress is the most early_press may be: past it a press well before the cancel point would cancel, and the timing would stop mattering.
const maxEarlyPress = 500 * time.Millisecond

type actionsYAML struct {
	Cancel     *float64       `yaml:"cancel"`
	EarlyPress *time.Duration `yaml:"early_press"`
}

// loadActions reads actions.yaml before the classes and skills, whose actions take its cancel point unless they name their own; early_press is the client's alone.
func (l *loader) loadActions(dir string) error {
	var y actionsYAML
	if err := readYAML(dir, actionsFile, &y); err != nil {
		return err
	}
	switch {
	case y.Cancel == nil || y.EarlyPress == nil:
		return fmt.Errorf("%s: needs cancel and early_press", actionsFile)
	case !isShare(*y.Cancel):
		return fmt.Errorf("%s: cancel must be a share of a length, from 0 to 1", actionsFile)
	case *y.EarlyPress < 0 || *y.EarlyPress > maxEarlyPress:
		return fmt.Errorf("%s: early_press must be from 0 to %s", actionsFile, maxEarlyPress)
	}
	l.cancel = *y.Cancel
	return nil
}

// timingYAML is an action's length and, where it names them, its hit and cancel point.
type timingYAML struct {
	Length time.Duration `yaml:"length"`
	Hit    *float64      `yaml:"hit"`
	Cancel *float64      `yaml:"cancel"`
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
	return t
}

func (raw timingYAML) named() bool { return raw.Length != 0 || raw.Hit != nil || raw.Cancel != nil }

// checkTiming refuses a length that isn't positive, a share outside 0 to 1, and a hit after the cancel point; the world refuses a length under its tick.
func checkTiming(t gamedata.Timing) error {
	switch {
	case t.Length <= 0:
		return errors.New("length must be positive")
	case !isShare(t.Hit) || !isShare(t.Cancel):
		return errors.New("hit and cancel are shares of the length, from 0 to 1")
	case t.Hit > t.Cancel:
		return errors.New("the hit must land by the cancel point")
	}
	return nil
}

func isShare(v float64) bool { return v >= 0 && v <= 1 }
