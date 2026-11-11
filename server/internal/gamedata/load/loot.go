package load

import (
	"fmt"
	"time"

	"finga/internal/gamedata"
)

const lootFile = "loot.yaml"

type lootYAML struct {
	CollectDelay   time.Duration `yaml:"collect_delay"`
	DropLife       time.Duration `yaml:"drop_life"`
	RoomCheckEvery time.Duration `yaml:"room_check_every"`
}

func (l *loader) loadLoot(dir string) error {
	var y lootYAML
	if err := readYAML(dir, lootFile, &y); err != nil {
		return err
	}
	for _, v := range []struct {
		name  string
		value time.Duration
	}{{"collect_delay", y.CollectDelay}, {"drop_life", y.DropLife}, {"room_check_every", y.RoomCheckEvery}} {
		if v.value <= 0 {
			return fmt.Errorf("%s: %s must be positive", lootFile, v.name)
		}
	}
	// A drop must have time to fly, and to be asked about again, before it is lost.
	if y.CollectDelay >= y.DropLife || y.RoomCheckEvery >= y.DropLife {
		return fmt.Errorf("%s: collect_delay and room_check_every must be shorter than drop_life", lootFile)
	}
	l.loot = gamedata.Loot(y)
	return nil
}
