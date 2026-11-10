package gamedata

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"finga/internal/kernel"
)

// The names the data files give the stats.
var (
	primaryNames = []string{"str", "dex", "int", "agi", "vit"}
	rateNames    = []string{"crit_rate", "attack_speed", "cooldown_speed", "move_speed"}
)

func (s *Stats) field(name string) *int {
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

func (p *PerPrimary[T]) field(name string) *T {
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

func (f *Flats[T]) field(name string) *T {
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

func (r *Rates[T]) field(name string) *T {
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

// rollField is the range an item's stat rolls in: any of the five primary and the sixteen derived.
func rollField(r *Rolls, name string) *Range {
	if name == "penetration" {
		return &r.Penetration
	}
	primary := PerPrimary[*Range]{STR: &r.STR, DEX: &r.DEX, INT: &r.INT, AGI: &r.AGI, VIT: &r.VIT}
	if p := primary.field(name); p != nil {
		return *p
	}
	flat := Flats[*Range]{
		PhysicalAttack: &r.PhysicalAttack, MagicAttack: &r.MagicAttack, PhysicalDefense: &r.PhysicalDefense, MagicDefense: &r.MagicDefense,
		MaxHP: &r.MaxHP, MaxMP: &r.MaxMP, Accuracy: &r.Accuracy, Evasion: &r.Evasion, CritDamage: &r.CritDamage, HPRegen: &r.HPRegen, MPRegen: &r.MPRegen,
	}
	if f := flat.field(name); f != nil {
		return *f
	}
	rates := Rates[*Range]{CritRate: &r.CritRate, AttackSpeed: &r.AttackSpeed, CooldownSpeed: &r.CooldownSpeed, MoveSpeed: &r.MoveSpeed}
	if f := rates.field(name); f != nil {
		return *f
	}
	return nil
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

// loadStats reads what each point gives, the curves, the limits and each class's bases, and checks every class's auto-assign split against the points a level gives.
func (d *Data) loadStats(dir string) error {
	var file statsYAML
	if err := readYAML(dir, statsFile, &file); err != nil {
		return err
	}
	r := StatRules{PointsPerLevel: file.PointsPerLevel, Classes: map[kernel.Class]Flats[Grows]{}}
	if r.PointsPerLevel < 1 {
		return fmt.Errorf("%s: points_per_level must be at least 1", statsFile)
	}
	for _, stat := range slices.Sorted(maps.Keys(file.PerPoint)) {
		per := r.PerPoint.field(stat)
		if per == nil {
			return fmt.Errorf("%s: per_point: unknown primary stat %q", statsFile, stat)
		}
		for _, name := range slices.Sorted(maps.Keys(file.PerPoint[stat])) {
			f := per.field(name)
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
		if _, ok := d.classIdx[id]; !ok {
			return fmt.Errorf("%s: classes: %s is not in %s", statsFile, id, classesFile)
		}
		var bases Flats[Grows]
		for _, stat := range slices.Sorted(maps.Keys(file.Classes[name])) {
			f := bases.field(stat)
			if f == nil {
				return fmt.Errorf("%s: classes: %s: unknown stat %q (a class has bases for flat stats)", statsFile, id, stat)
			}
			*f = Grows(file.Classes[name][stat])
		}
		r.Classes[id] = bases
	}
	for _, c := range d.classes {
		if _, ok := r.Classes[c.ID]; !ok {
			return fmt.Errorf("%s: classes: %s has no bases", statsFile, c.ID)
		}
		if got := c.AutoAssign.Total(); got != r.PointsPerLevel {
			return fmt.Errorf("%s: class %s: auto_assign must add up to points_per_level (%d), got %d", classesFile, c.ID, r.PointsPerLevel, got)
		}
	}
	d.stats = r
	return nil
}

// loadCurves reads the four rates' curves: each over a primary stat, with a limit above its base and a positive k.
func loadCurves(r *StatRules, raw map[string]curveYAML) error {
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		if r.Curves.field(name) == nil {
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
		*r.Curves.field(name) = Curve{Stat: Primary(stat), Base: c.Base, Limit: c.Limit, K: c.K}
	}
	return nil
}

// loadRequires reads an item's stat minimums: primary stats only, each at least 1.
func loadRequires(raw map[string]int) (Stats, error) {
	var s Stats
	for _, name := range slices.Sorted(maps.Keys(raw)) {
		f := s.field(name)
		if f == nil {
			return s, fmt.Errorf("requires: unknown primary stat %q", name)
		}
		if raw[name] < 1 {
			return s, fmt.Errorf("requires: %s must be at least 1", name)
		}
		*f = raw[name]
	}
	return s, nil
}

// loadRolls reads an equipment item's stat ranges: at least one, each [min, max] with 0 <= min <= max and max > 0.
func loadRolls(stats map[string][]int) (Rolls, error) {
	var r Rolls
	if len(stats) == 0 {
		return r, errors.New("equipment needs stats")
	}
	for _, name := range slices.Sorted(maps.Keys(stats)) {
		field := rollField(&r, name)
		if field == nil {
			return r, fmt.Errorf("unknown stat %s", name)
		}
		v := stats[name]
		if len(v) != 2 || v[0] < 0 || v[0] > v[1] || v[1] == 0 {
			return r, fmt.Errorf("stat %s needs a range [min, max] with 0 <= min <= max and max above 0, got %v", name, v)
		}
		*field = Range{Min: v[0], Max: v[1]}
	}
	return r, nil
}
