package load

import (
	"fmt"
	"maps"
	"slices"

	"finga/internal/gamedata"
	"finga/internal/kernel"
)

const statsFile = "stats.yaml"

// The names the data files give the stats.
var (
	primaryNames = []string{"str", "dex", "int", "agi", "vit"}
	rateNames    = []string{"crit_rate", "attack_speed", "cooldown_speed", "move_speed"}
)

// primaryYAML is the five primary stats as the data files write them.
type primaryYAML struct {
	STR int `yaml:"str"`
	DEX int `yaml:"dex"`
	INT int `yaml:"int"`
	AGI int `yaml:"agi"`
	VIT int `yaml:"vit"`
}

type statsYAML struct {
	PointsPerLevel int                             `yaml:"points_per_level"`
	PerPoint       map[string]map[string]float64   `yaml:"per_point"`
	Diminishing    map[string]curveYAML            `yaml:"diminishing"`
	Limits         limitsYAML                      `yaml:"limits"`
	Classes        map[string]map[string]growsYAML `yaml:"classes"`
}

type curveYAML struct {
	Stat  string  `yaml:"stat"`
	Base  float64 `yaml:"base"`
	Limit float64 `yaml:"limit"`
	K     float64 `yaml:"k"`
}

type limitsYAML struct {
	Penetration float64   `yaml:"penetration"`
	HitChance   []float64 `yaml:"hit_chance"`
}

type growsYAML struct {
	Base     float64 `yaml:"base"`
	PerLevel float64 `yaml:"per_level"`
}

func statField(s *gamedata.Stats, name string) *int {
	switch name {
	case "str":
		return &s.STR
	case "dex":
		return &s.DEX
	case "int":
		return &s.INT
	case "agi":
		return &s.AGI
	case "vit":
		return &s.VIT
	}
	return nil
}

func primaryField[T any](p *gamedata.PerPrimary[T], name string) *T {
	switch name {
	case "str":
		return &p.STR
	case "dex":
		return &p.DEX
	case "int":
		return &p.INT
	case "agi":
		return &p.AGI
	case "vit":
		return &p.VIT
	}
	return nil
}

func flatField[T any](f *gamedata.Flats[T], name string) *T {
	switch name {
	case "physical_attack":
		return &f.PhysicalAttack
	case "magic_attack":
		return &f.MagicAttack
	case "physical_defense":
		return &f.PhysicalDefense
	case "magic_defense":
		return &f.MagicDefense
	case "max_hp":
		return &f.MaxHP
	case "max_mp":
		return &f.MaxMP
	case "accuracy":
		return &f.Accuracy
	case "evasion":
		return &f.Evasion
	case "crit_damage":
		return &f.CritDamage
	case "hp_regen":
		return &f.HPRegen
	case "mp_regen":
		return &f.MPRegen
	}
	return nil
}

func rateField[T any](r *gamedata.Rates[T], name string) *T {
	switch name {
	case "crit_rate":
		return &r.CritRate
	case "attack_speed":
		return &r.AttackSpeed
	case "cooldown_speed":
		return &r.CooldownSpeed
	case "move_speed":
		return &r.MoveSpeed
	}
	return nil
}

// loadStats reads what each point gives, the curves, the limits and each class's bases, and checks every class's auto-assign split against the points a level gives.
func (l *loader) loadStats(dir string) error {
	var file statsYAML
	if err := readYAML(dir, statsFile, &file); err != nil {
		return err
	}
	r := gamedata.StatRules{PointsPerLevel: file.PointsPerLevel, Classes: map[kernel.Class]gamedata.Flats[gamedata.Grows]{}}
	if r.PointsPerLevel < 1 {
		return fmt.Errorf("%s: points_per_level must be at least 1", statsFile)
	}
	for _, stat := range slices.Sorted(maps.Keys(file.PerPoint)) {
		per := primaryField(&r.PerPoint, stat)
		if per == nil {
			return fmt.Errorf("%s: per_point: unknown primary stat %q", statsFile, stat)
		}
		for _, name := range slices.Sorted(maps.Keys(file.PerPoint[stat])) {
			f := flatField(per, name)
			if f == nil {
				return fmt.Errorf("%s: per_point: %s: unknown stat %q (a point gives flat stats; rates have their curves)", statsFile, stat, name)
			}
			if v := file.PerPoint[stat][name]; v >= 0 {
				*f = v
			} else {
				return fmt.Errorf("%s: per_point: %s: %s must not be negative", statsFile, stat, name)
			}
		}
	}
	if err := loadCurves(&r, file.Diminishing); err != nil {
		return err
	}
	if p := file.Limits.Penetration; p <= 0 || p > 1 {
		return fmt.Errorf("%s: limits: penetration must be within (0, 1]", statsFile)
	}
	r.Penetration = file.Limits.Penetration
	if h := file.Limits.HitChance; len(h) != 2 || h[0] <= 0 || h[0] >= h[1] || h[1] > 1 {
		return fmt.Errorf("%s: limits: hit_chance must be [min, max] with 0 < min < max <= 1", statsFile)
	}
	r.MinHit, r.MaxHit = file.Limits.HitChance[0], file.Limits.HitChance[1]
	for _, name := range slices.Sorted(maps.Keys(file.Classes)) {
		id, err := kernel.ParseClass(name)
		if err != nil {
			return fmt.Errorf("%s: classes: %q is not a class", statsFile, name)
		}
		if !l.classes.has(id) {
			return fmt.Errorf("%s: classes: %s is not in %s", statsFile, id, classesFile)
		}
		var bases gamedata.Flats[gamedata.Grows]
		for _, stat := range slices.Sorted(maps.Keys(file.Classes[name])) {
			f := flatField(&bases, stat)
			if f == nil {
				return fmt.Errorf("%s: classes: %s: unknown stat %q (a class has bases for flat stats)", statsFile, id, stat)
			}
			*f = gamedata.Grows(file.Classes[name][stat])
		}
		r.Classes[id] = bases
	}
	for _, c := range l.classes.list {
		if _, ok := r.Classes[c.ID]; !ok {
			return fmt.Errorf("%s: classes: %s has no bases", statsFile, c.ID)
		}
		if got := c.AutoAssign.Total(); got != r.PointsPerLevel {
			return fmt.Errorf("%s: class %s: auto_assign must add up to points_per_level (%d), got %d", classesFile, c.ID, r.PointsPerLevel, got)
		}
	}
	l.stats = r
	return nil
}

// loadCurves reads the four rates' curves: each over a primary stat, with a limit above its base and a positive k.
func loadCurves(r *gamedata.StatRules, raw map[string]curveYAML) error {
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		if rateField(&r.Curves, name) == nil {
			return fmt.Errorf("%s: diminishing: unknown rate %q", statsFile, name)
		}
	}
	for _, name := range rateNames {
		c, ok := raw[name]
		if !ok {
			return fmt.Errorf("%s: diminishing: missing %s", statsFile, name)
		}
		stat := slices.Index(primaryNames, c.Stat)
		switch {
		case stat < 0:
			return fmt.Errorf("%s: diminishing: %s: unknown primary stat %q", statsFile, name, c.Stat)
		case c.Base < 0 || c.Limit <= c.Base:
			return fmt.Errorf("%s: diminishing: %s: limit must be above base, and base not negative", statsFile, name)
		case c.K <= 0:
			return fmt.Errorf("%s: diminishing: %s: k must be positive", statsFile, name)
		}
		*rateField(&r.Curves, name) = gamedata.Curve{Stat: gamedata.Primary(stat), Base: c.Base, Limit: c.Limit, K: c.K}
	}
	return nil
}
