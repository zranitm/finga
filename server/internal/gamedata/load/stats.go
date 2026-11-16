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
	rateNames    = []string{"crit_rate", "cooldown_speed", "move_speed"}
	flatNames    = []string{"physical_attack", "magic_attack", "physical_defense", "magic_defense", "max_hp", "accuracy", "evasion", "crit_damage", "hp_regen"}
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
	PointsPerLevel      int                             `yaml:"points_per_level"`
	SkillPointsPerLevel int                             `yaml:"skill_points_per_level"`
	PerPoint            map[string]map[string]float64   `yaml:"per_point"`
	Diminishing         map[string]curveYAML            `yaml:"diminishing"`
	Hit                 hitYAML                         `yaml:"hit"`
	Limits              limitsYAML                      `yaml:"limits"`
	Hardiest            string                          `yaml:"hardiest"`
	Classes             map[string]map[string]growsYAML `yaml:"classes"`
}

type curveYAML struct {
	Stat  string  `yaml:"stat"`
	Base  float64 `yaml:"base"`
	Limit float64 `yaml:"limit"`
	K     float64 `yaml:"k"`
}

type hitYAML struct {
	Top   float64 `yaml:"top"`
	K     float64 `yaml:"k"`
	Power float64 `yaml:"power"`
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
	case "accuracy":
		return &f.Accuracy
	case "evasion":
		return &f.Evasion
	case "crit_damage":
		return &f.CritDamage
	case "hp_regen":
		return &f.HPRegen
	}
	return nil
}

func rateField[T any](r *gamedata.Rates[T], name string) *T {
	switch name {
	case "crit_rate":
		return &r.CritRate
	case "cooldown_speed":
		return &r.CooldownSpeed
	case "move_speed":
		return &r.MoveSpeed
	}
	return nil
}

// loadStats reads what each point gives, the curves, the limits and each class's bases, none below its parent's and no max HP growing faster than the hardiest's, and checks every class's auto-assign split against the points a level gives.
func (l *loader) loadStats(dir string) error {
	var file statsYAML
	if err := readYAML(dir, statsFile, &file); err != nil {
		return err
	}
	r := gamedata.StatRules{PointsPerLevel: file.PointsPerLevel, SkillPointsPerLevel: file.SkillPointsPerLevel, Classes: map[kernel.Class]gamedata.Flats[gamedata.Grows]{}}
	if r.PointsPerLevel < 1 || r.SkillPointsPerLevel < 1 {
		return fmt.Errorf("%s: points_per_level and skill_points_per_level must each be at least 1", statsFile)
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
			if v := file.PerPoint[stat][name]; nonNegative(v) {
				*f = v
			} else {
				return fmt.Errorf("%s: per_point: %s: %s must not be negative", statsFile, stat, name)
			}
		}
	}
	if err := loadCurves(&r, file.Diminishing); err != nil {
		return err
	}
	if p := file.Limits.Penetration; !(p > 0 && p <= 1) {
		return fmt.Errorf("%s: limits: penetration must be within (0, 1]", statsFile)
	}
	r.Penetration = file.Limits.Penetration
	if h := file.Limits.HitChance; len(h) != 2 || !(h[0] > 0 && h[0] < h[1] && h[1] <= 1) {
		return fmt.Errorf("%s: limits: hit_chance must be [min, max] with 0 < min < max <= 1", statsFile)
	}
	if h := file.Hit; !(h.Top > 0 && h.Top <= 1) || !positive(h.K) || !positive(h.Power) {
		return fmt.Errorf("%s: hit: top must be within (0, 1], and k and power above 0", statsFile)
	}
	r.Hit = gamedata.HitCurve{Top: file.Hit.Top, K: file.Hit.K, Power: file.Hit.Power, Min: file.Limits.HitChance[0], Max: file.Limits.HitChance[1]}
	for _, name := range slices.Sorted(maps.Keys(file.Classes)) {
		id := kernel.Class(name)
		if !l.classes.has(id) {
			return fmt.Errorf("%s: classes: %s is not in %s", statsFile, id, classesFile)
		}
		var bases gamedata.Flats[gamedata.Grows]
		for _, stat := range slices.Sorted(maps.Keys(file.Classes[name])) {
			f := flatField(&bases, stat)
			if f == nil {
				return fmt.Errorf("%s: classes: %s: unknown stat %q (a class has bases for flat stats)", statsFile, id, stat)
			}
			g := file.Classes[name][stat]
			if !finite(g.Base) || !finite(g.PerLevel) {
				return fmt.Errorf("%s: classes: %s: %s must be numbers", statsFile, id, stat)
			}
			*f = gamedata.Grows(g)
		}
		r.Classes[id] = bases
	}
	for _, c := range l.classes.list {
		if _, ok := r.Classes[c.ID]; !ok {
			return fmt.Errorf("%s: classes: %s has no bases", statsFile, c.ID)
		}
		if got := c.AutoAssign.Total(); c.Parent != "" && got != r.PointsPerLevel {
			return fmt.Errorf("%s: class %s: auto_assign must add up to points_per_level (%d), got %d", classesFile, c.ID, r.PointsPerLevel, got)
		}
		if err := notBelowParent(r, c); err != nil {
			return err
		}
	}
	if err := noneHardier(r, kernel.Class(file.Hardiest)); err != nil {
		return err
	}
	l.stats = r
	return nil
}

// noneHardier refuses a class whose max HP grows faster a level than the hardiest class's, which the data names.
func noneHardier(r gamedata.StatRules, hardiest kernel.Class) error {
	most, ok := r.Classes[hardiest]
	if !ok {
		return fmt.Errorf("%s: hardiest must name a class with bases, got %q", statsFile, hardiest)
	}
	for _, id := range slices.Sorted(maps.Keys(r.Classes)) {
		if g := r.Classes[id].MaxHP.PerLevel; g > most.MaxHP.PerLevel {
			return fmt.Errorf("%s: classes: %s: max_hp per_level %g is above %s's %g, the hardiest's", statsFile, id, g, hardiest, most.MaxHP.PerLevel)
		}
	}
	return nil
}

// notBelowParent refuses a class with a base or a per_level below its parent's, so a promotion never lowers a value.
func notBelowParent(r gamedata.StatRules, c gamedata.Class) error {
	if c.Parent == "" {
		return nil
	}
	own, parent := r.Classes[c.ID], r.Classes[c.Parent]
	for _, name := range flatNames {
		g, p := *flatField(&own, name), *flatField(&parent, name)
		if g.Base < p.Base || g.PerLevel < p.PerLevel {
			return fmt.Errorf("%s: classes: %s: %s (base %g, per_level %g) is below %s's (base %g, per_level %g); a promotion must not lower a value", statsFile, c.ID, name, g.Base, g.PerLevel, c.Parent, p.Base, p.PerLevel)
		}
	}
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
		case !nonNegative(c.Base) || !(c.Limit > c.Base) || !finite(c.Limit):
			return fmt.Errorf("%s: diminishing: %s: limit must be above base, and base not negative", statsFile, name)
		case !positive(c.K):
			return fmt.Errorf("%s: diminishing: %s: k must be positive", statsFile, name)
		}
		*rateField(&r.Curves, name) = gamedata.Curve{Stat: gamedata.Primary(stat), Base: c.Base, Limit: c.Limit, K: c.K}
	}
	return nil
}
