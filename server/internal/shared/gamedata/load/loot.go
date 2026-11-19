package load

import (
	"fmt"
	"time"

	"finga/internal/shared/gamedata"
)

const lootFile = "loot.yaml"

type lootYAML struct {
	Pouch struct {
		Normal float64 `yaml:"normal"`
		Elite  float64 `yaml:"elite"`
	} `yaml:"pouch"`
	Reach     float64       `yaml:"reach"`
	PouchLife time.Duration `yaml:"pouch_life"`
	BoxLife   time.Duration `yaml:"box_life"`
}

func (l *loader) loadLoot(dir string) error {
	var y lootYAML
	if err := readYAML(dir, lootFile, &y); err != nil {
		return err
	}
	for _, c := range []struct {
		name   string
		chance float64
	}{{"pouch.normal", y.Pouch.Normal}, {"pouch.elite", y.Pouch.Elite}} {
		if !(c.chance > 0 && c.chance <= 1) {
			return fmt.Errorf("%s: %s must be in (0, 1]", lootFile, c.name)
		}
	}
	for _, v := range []struct {
		name  string
		value time.Duration
	}{{"pouch_life", y.PouchLife}, {"box_life", y.BoxLife}} {
		if v.value <= 0 {
			return fmt.Errorf("%s: %s must be positive", lootFile, v.name)
		}
	}
	l.loot = gamedata.Loot{Pouch: gamedata.PouchChance{Normal: y.Pouch.Normal, Elite: y.Pouch.Elite}, Reach: y.Reach, PouchLife: y.PouchLife, BoxLife: y.BoxLife}
	return nil
}

// checkLootReach refuses a reach short of the talk reach, so a pouch at a character's feet is always in reach.
func (l *loader) checkLootReach(string) error {
	if !(l.loot.Reach >= l.talkReach) {
		return fmt.Errorf("%s: reach %v must be at least the talk reach, %v", lootFile, l.loot.Reach, l.talkReach)
	}
	return nil
}
