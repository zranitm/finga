package gamedata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"gopkg.in/yaml.v3"

	"finga/internal/application/world/sim"
	"finga/internal/model"
)

// File names inside the data directory (page 2).
const (
	movementFile = "movement.yaml"
	classesFile  = "classes.yaml"
	statsFile    = "stats.yaml"
	expCurveFile = "exp_curve.yaml"
	itemsFile    = "items.yaml"
	mobsFile     = "mobs.yaml"
	mapsDir      = "maps"
	mapExt       = ".tmj"

	levelUpPoints   = 5 // the Stage 1 preset adds 5 points per level-up
	defaultMaxStack = 100
)

// Load reads and validates every file under dir.
func Load(dir string) (*Data, error) {
	d := &Data{
		classIdx: map[model.Class]int{},
		formulas: map[model.Class]Formulas{},
		itemIdx:  map[string]int{},
		mobIdx:   map[string]int{},
		mapIdx:   map[string]int{},
	}
	steps := []func(string) error{d.loadMovement, d.loadClasses, d.loadStats, d.loadExpCurve, d.loadItems, d.validateStarters, d.loadMobs, d.loadMaps}
	for _, step := range steps {
		if err := step(dir); err != nil {
			return nil, fmt.Errorf("gamedata: %w", err)
		}
	}
	return d, nil
}

// readYAML decodes one file strictly: an unknown key is an error.
func readYAML(dir, name string, into any) error {
	f, err := os.Open(filepath.Join(dir, name))
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	if err := dec.Decode(into); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

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

func (d *Data) loadMovement(dir string) error {
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
		if v.value <= 0 {
			return fmt.Errorf("%s: %s must be positive", movementFile, v.name)
		}
	}
	d.movement = sim.Params{
		Dt: m.Dt, WalkSpeed: m.WalkSpeed, JumpVelocity: m.JumpVelocity, Gravity: m.Gravity, MaxFallSpeed: m.MaxFallSpeed,
		ClimbSpeed: m.ClimbSpeed, DropThrough: m.DropThrough, Width: m.Width, Height: m.Height,
	}
	return nil
}

type classesYAML struct {
	Classes []classYAML `yaml:"classes"`
}

type classYAML struct {
	ID            string     `yaml:"id"`
	Name          string     `yaml:"name"`
	Attack        attackYAML `yaml:"attack"`
	BaseStats     Stats      `yaml:"base_stats"`
	LevelUpPoints Stats      `yaml:"level_up_points"`
	StarterItems  []Grant    `yaml:"starter_items"`
}

type attackYAML struct {
	Kind     string        `yaml:"kind"`
	Width    float64       `yaml:"width"`
	Height   float64       `yaml:"height"`
	Range    float64       `yaml:"range"`
	Cooldown time.Duration `yaml:"cooldown"`
}

func (d *Data) loadClasses(dir string) error {
	var file classesYAML
	if err := readYAML(dir, classesFile, &file); err != nil {
		return err
	}
	if len(file.Classes) == 0 {
		return fmt.Errorf("%s: no classes", classesFile)
	}
	for _, c := range file.Classes {
		id, err := model.ParseClass(c.ID)
		if err != nil {
			return fmt.Errorf("%s: class %q: not a class", classesFile, c.ID)
		}
		if _, dup := d.classIdx[id]; dup {
			return fmt.Errorf("%s: duplicate class %s", classesFile, id)
		}
		kind := AttackKind(c.Attack.Kind)
		switch kind {
		case Melee:
			if c.Attack.Width <= 0 || c.Attack.Height <= 0 {
				return fmt.Errorf("%s: class %s: a melee attack needs width and height", classesFile, id)
			}
		case Ranged:
			if c.Attack.Range <= 0 {
				return fmt.Errorf("%s: class %s: a ranged attack needs a range", classesFile, id)
			}
		default:
			return fmt.Errorf("%s: class %s: unknown attack kind %q", classesFile, id, c.Attack.Kind)
		}
		if c.Attack.Cooldown <= 0 {
			return fmt.Errorf("%s: class %s: cooldown must be positive", classesFile, id)
		}
		if got := c.LevelUpPoints.Total(); got != levelUpPoints {
			return fmt.Errorf("%s: class %s: level_up_points must add up to %d, got %d", classesFile, id, levelUpPoints, got)
		}
		d.classIdx[id] = len(d.classes)
		d.classes = append(d.classes, Class{
			ID: id, Name: c.Name,
			Attack:        Attack{Kind: kind, Width: c.Attack.Width, Height: c.Attack.Height, Range: c.Attack.Range, Cooldown: c.Attack.Cooldown},
			BaseStats:     c.BaseStats,
			LevelUpPoints: c.LevelUpPoints,
			StarterItems:  c.StarterItems,
		})
	}
	return nil
}

type formulaYAML struct {
	Base     float64            `yaml:"base"`
	PerLevel float64            `yaml:"per_level"`
	Coef     map[string]float64 `yaml:"coef"`
	Cap      float64            `yaml:"cap"`
}

// derivedStats are the nine formula names, in page 5's order.
var derivedStats = []string{"max_hp", "max_mp", "attack_power", "accuracy", "evasion", "crit_rate", "crit_damage", "defense", "penetration"}

func (d *Data) loadStats(dir string) error {
	var file map[string]map[string]formulaYAML
	if err := readYAML(dir, statsFile, &file); err != nil {
		return err
	}
	for _, className := range slices.Sorted(sortedKeys(file)) {
		id, err := model.ParseClass(className)
		if err != nil {
			return fmt.Errorf("%s: class %q: not a class", statsFile, className)
		}
		if _, ok := d.classIdx[id]; !ok {
			return fmt.Errorf("%s: class %s: not in %s", statsFile, id, classesFile)
		}
		stats := file[className]
		for _, name := range slices.Sorted(sortedKeys(stats)) {
			if !slices.Contains(derivedStats, name) {
				return fmt.Errorf("%s: class %s: unknown derived stat %q", statsFile, id, name)
			}
		}
		var f Formulas
		for _, name := range derivedStats {
			raw, ok := stats[name]
			if !ok {
				return fmt.Errorf("%s: class %s: missing formula %s", statsFile, id, name)
			}
			formula, err := toFormula(raw)
			if err != nil {
				return fmt.Errorf("%s: class %s: %s: %w", statsFile, id, name, err)
			}
			*formulaField(&f, name) = formula
		}
		d.formulas[id] = f
	}
	for _, c := range d.classes {
		if _, ok := d.formulas[c.ID]; !ok {
			return fmt.Errorf("%s: class %s: no formulas", statsFile, c.ID)
		}
	}
	return nil
}

func toFormula(raw formulaYAML) (Formula, error) {
	f := Formula{Base: raw.Base, PerLevel: raw.PerLevel, Cap: raw.Cap}
	for _, stat := range slices.Sorted(sortedKeys(raw.Coef)) {
		v := raw.Coef[stat]
		switch stat {
		case "str":
			f.Coef.STR = v
		case "dex":
			f.Coef.DEX = v
		case "int":
			f.Coef.INT = v
		case "agi":
			f.Coef.AGI = v
		case "vit":
			f.Coef.VIT = v
		default:
			return Formula{}, fmt.Errorf("unknown primary stat %s", stat)
		}
	}
	return f, nil
}

func formulaField(f *Formulas, name string) *Formula {
	switch name {
	case "max_hp":
		return &f.MaxHP
	case "max_mp":
		return &f.MaxMP
	case "attack_power":
		return &f.AttackPower
	case "accuracy":
		return &f.Accuracy
	case "evasion":
		return &f.Evasion
	case "crit_rate":
		return &f.CritRate
	case "crit_damage":
		return &f.CritDamage
	case "defense":
		return &f.Defense
	default:
		return &f.Penetration
	}
}

type expCurveYAML struct {
	LevelCap  int `yaml:"level_cap"`
	ExpToNext []struct {
		Level int   `yaml:"level"`
		Exp   int64 `yaml:"exp"`
	} `yaml:"exp_to_next"`
}

func (d *Data) loadExpCurve(dir string) error {
	var file expCurveYAML
	if err := readYAML(dir, expCurveFile, &file); err != nil {
		return err
	}
	if file.LevelCap < 2 {
		return fmt.Errorf("%s: level_cap must be at least 2", expCurveFile)
	}
	d.levelCap = file.LevelCap
	d.expToNext = make([]int64, file.LevelCap-1)
	seen := make([]bool, file.LevelCap-1)
	for _, e := range file.ExpToNext {
		if e.Level < 1 || e.Level >= file.LevelCap {
			return fmt.Errorf("%s: level %d is at or past the cap %d", expCurveFile, e.Level, file.LevelCap)
		}
		if seen[e.Level-1] {
			return fmt.Errorf("%s: duplicate level %d", expCurveFile, e.Level)
		}
		if e.Exp < 1 {
			return fmt.Errorf("%s: level %d must need positive EXP", expCurveFile, e.Level)
		}
		seen[e.Level-1] = true
		d.expToNext[e.Level-1] = e.Exp
	}
	for level := 1; level < file.LevelCap; level++ {
		if !seen[level-1] {
			return fmt.Errorf("%s: no entry for level %d", expCurveFile, level)
		}
		if level > 1 && d.expToNext[level-1] <= d.expToNext[level-2] {
			return fmt.Errorf("%s: level %d must need more EXP than level %d", expCurveFile, level, level-1)
		}
	}
	return nil
}

type itemsYAML struct {
	Items []itemYAML `yaml:"items"`
}

type itemYAML struct {
	ID       string `yaml:"id"`
	Name     string `yaml:"name"`
	Tab      string `yaml:"tab"`
	Slot     string `yaml:"slot"`
	Class    string `yaml:"class"`
	MinLevel int    `yaml:"min_level"`
	Attack   int    `yaml:"attack"`
	Defense  int    `yaml:"defense"`
	HealHP   int    `yaml:"heal_hp"`
	HealMP   int    `yaml:"heal_mp"`
	MaxStack int    `yaml:"max_stack"`
}

func (d *Data) loadItems(dir string) error {
	var file itemsYAML
	if err := readYAML(dir, itemsFile, &file); err != nil {
		return err
	}
	for _, raw := range file.Items {
		if raw.ID == "" {
			return fmt.Errorf("%s: an item has no id", itemsFile)
		}
		if _, dup := d.itemIdx[raw.ID]; dup {
			return fmt.Errorf("%s: duplicate item %s", itemsFile, raw.ID)
		}
		it := Item{ID: raw.ID, Name: raw.Name, Tab: Tab(raw.Tab), MinLevel: raw.MinLevel, Attack: raw.Attack, Defense: raw.Defense, HealHP: raw.HealHP, HealMP: raw.HealMP, MaxStack: raw.MaxStack}
		switch it.Tab {
		case Equip:
			it.Slot = Slot(raw.Slot)
			if it.Slot != Weapon && it.Slot != Armor {
				return fmt.Errorf("%s: item %s: equipment needs a slot (weapon or armor), got %q", itemsFile, it.ID, raw.Slot)
			}
			if raw.Class != "" {
				class, err := model.ParseClass(raw.Class)
				if err != nil {
					return fmt.Errorf("%s: item %s: unknown class %q", itemsFile, it.ID, raw.Class)
				}
				if _, ok := d.classIdx[class]; !ok {
					return fmt.Errorf("%s: item %s: class %s is not in %s", itemsFile, it.ID, class, classesFile)
				}
				it.Class = class
			}
			it.MaxStack = 1 // equipment never stacks
		case Use, Etc:
			if raw.Slot != "" || raw.Class != "" {
				return fmt.Errorf("%s: item %s: only equipment has a slot or a class", itemsFile, it.ID)
			}
			if it.MaxStack == 0 {
				it.MaxStack = defaultMaxStack
			}
			if it.MaxStack < 1 {
				return fmt.Errorf("%s: item %s: max_stack must be positive", itemsFile, it.ID)
			}
		default:
			return fmt.Errorf("%s: item %s: unknown tab %q", itemsFile, it.ID, raw.Tab)
		}
		d.itemIdx[it.ID] = len(d.items)
		d.items = append(d.items, it)
	}
	return nil
}

type mobsYAML struct {
	Mobs []mobYAML `yaml:"mobs"`
}

type mobYAML struct {
	ID       string     `yaml:"id"`
	Name     string     `yaml:"name"`
	Level    int        `yaml:"level"`
	HP       int        `yaml:"hp"`
	Attack   int        `yaml:"attack"`
	Defense  int        `yaml:"defense"`
	Accuracy int        `yaml:"accuracy"`
	Evasion  int        `yaml:"evasion"`
	Exp      int64      `yaml:"exp"`
	Speed    float64    `yaml:"speed"`
	Width    float64    `yaml:"width"`
	Height   float64    `yaml:"height"`
	Behavior string     `yaml:"behavior"`
	Drops    []dropYAML `yaml:"drops"`
}

type dropYAML struct {
	Item   string  `yaml:"item"`
	Chance float64 `yaml:"chance"`
	Min    int     `yaml:"min"`
	Max    int     `yaml:"max"`
}

func (d *Data) loadMobs(dir string) error {
	var file mobsYAML
	if err := readYAML(dir, mobsFile, &file); err != nil {
		return err
	}
	for _, raw := range file.Mobs {
		if raw.ID == "" {
			return fmt.Errorf("%s: a mob has no id", mobsFile)
		}
		if _, dup := d.mobIdx[raw.ID]; dup {
			return fmt.Errorf("%s: duplicate mob %s", mobsFile, raw.ID)
		}
		if raw.Level < 1 || raw.HP < 1 || raw.Width <= 0 || raw.Height <= 0 || raw.Speed < 0 || raw.Exp < 0 {
			return fmt.Errorf("%s: mob %s: level and hp must be at least 1, width and height positive, speed and exp not negative", mobsFile, raw.ID)
		}
		if raw.Behavior != "patrol" {
			return fmt.Errorf("%s: mob %s: unknown behavior %q (Stage 1 has patrol)", mobsFile, raw.ID, raw.Behavior)
		}
		mob := Mob{
			ID: raw.ID, Name: raw.Name, Level: raw.Level, HP: raw.HP, Attack: raw.Attack, Defense: raw.Defense,
			Accuracy: raw.Accuracy, Evasion: raw.Evasion, Exp: raw.Exp, Speed: raw.Speed, Width: raw.Width, Height: raw.Height,
			Behavior: raw.Behavior,
		}
		for _, dr := range raw.Drops {
			item, ok := d.Item(dr.Item)
			if !ok {
				return fmt.Errorf("%s: mob %s: drop %s: unknown item", mobsFile, raw.ID, dr.Item)
			}
			if dr.Chance <= 0 || dr.Chance > 1 {
				return fmt.Errorf("%s: mob %s: drop %s: chance must be within (0, 1]", mobsFile, raw.ID, dr.Item)
			}
			if dr.Min < 1 || dr.Max < dr.Min {
				return fmt.Errorf("%s: mob %s: drop %s: need 1 <= min <= max", mobsFile, raw.ID, dr.Item)
			}
			if item.Tab == Equip && dr.Max != 1 {
				return fmt.Errorf("%s: mob %s: drop %s: equipment never stacks, max must be 1", mobsFile, raw.ID, dr.Item)
			}
			mob.Drops = append(mob.Drops, Drop(dr))
		}
		d.mobIdx[mob.ID] = len(d.mobs)
		d.mobs = append(d.mobs, mob)
	}
	return nil
}

func (d *Data) loadMaps(dir string) error {
	entries, err := os.ReadDir(filepath.Join(dir, mapsDir))
	if err != nil {
		return fmt.Errorf("%s: %w", mapsDir, err)
	}
	for _, e := range entries { // ReadDir sorts by name
		if e.IsDir() || filepath.Ext(e.Name()) != mapExt {
			continue
		}
		m, err := d.loadMap(filepath.Join(dir, mapsDir, e.Name()))
		if err != nil {
			return err
		}
		d.mapIdx[m.ID] = len(d.maps)
		d.maps = append(d.maps, m)
	}
	if len(d.maps) == 0 {
		return fmt.Errorf("%s: no %s maps", mapsDir, mapExt)
	}
	// Portals can only be checked once every map is loaded.
	for _, m := range d.maps {
		file := m.ID + mapExt
		for _, p := range m.Portals {
			target, ok := d.Map(p.TargetMap)
			if !ok {
				return fmt.Errorf("%s: layer %q: object %q: target map %q does not exist", file, layerPortals, p.Name, p.TargetMap)
			}
			if !slices.ContainsFunc(target.Portals, func(q Portal) bool { return q.Name == p.TargetPortal }) {
				return fmt.Errorf("%s: layer %q: object %q: target portal %q not in map %q", file, layerPortals, p.Name, p.TargetPortal, p.TargetMap)
			}
		}
	}
	if d.Town() == nil {
		return errors.New("maps: no map has kind town")
	}
	return nil
}

func sortedKeys[V any](m map[string]V) func(func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}

// validateStarters checks each class's starter items once the items are
// loaded: known templates, positive quantities, one of a piece of equipment.
func (d *Data) validateStarters(string) error {
	for _, c := range d.classes {
		for _, g := range c.StarterItems {
			it, ok := d.Item(g.Item)
			if !ok {
				return fmt.Errorf("%s: class %s: starter item %s: unknown item", classesFile, c.ID, g.Item)
			}
			if g.Quantity < 1 || g.Quantity > it.MaxStack {
				return fmt.Errorf("%s: class %s: starter item %s: quantity must be 1 to %d", classesFile, c.ID, g.Item, it.MaxStack)
			}
		}
	}
	return nil
}
