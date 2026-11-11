package load

import (
	"fmt"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
)

const classesFile = "classes.yaml"

type classesYAML struct {
	Classes []classYAML `yaml:"classes"`
}

type classYAML struct {
	ID           string      `yaml:"id"`
	Name         string      `yaml:"name"`
	Parent       string      `yaml:"parent"`
	Look         lookYAML    `yaml:"look"`
	Attack       attackYAML  `yaml:"attack"`
	BaseStats    primaryYAML `yaml:"base_stats"`
	AutoAssign   primaryYAML `yaml:"auto_assign"`
	StarterItems []grantYAML `yaml:"starter_items"`
}

type attackYAML struct {
	Kind     string        `yaml:"kind"`
	Damage   string        `yaml:"damage"`
	Width    float64       `yaml:"width"`
	Height   float64       `yaml:"height"`
	Range    float64       `yaml:"range"`
	Cooldown time.Duration `yaml:"cooldown"`
}

type grantYAML struct {
	Item     string `yaml:"item"`
	Quantity int    `yaml:"quantity"`
}

func grants(raw []grantYAML) []gamedata.Grant {
	var out []gamedata.Grant
	for _, g := range raw {
		out = append(out, gamedata.Grant(g))
	}
	return out
}

func (l *loader) loadClasses(dir string) error {
	var file classesYAML
	if err := readYAML(dir, classesFile, &file); err != nil {
		return err
	}
	if len(file.Classes) == 0 {
		return fmt.Errorf("%s: no classes", classesFile)
	}
	for i, c := range file.Classes {
		id := kernel.Class(c.ID)
		if c.ID == "" || c.Name == "" {
			return fmt.Errorf("%s: class %d: needs an id and a name", classesFile, i+1)
		}
		if l.classes.has(id) {
			return fmt.Errorf("%s: duplicate class %s", classesFile, id)
		}
		line, err := l.lineOf(i, id, kernel.Class(c.Parent))
		if err != nil {
			return err
		}
		root := len(line) == 1
		switch {
		case root != (c.BaseStats != primaryYAML{}):
			return fmt.Errorf("%s: class %s: the root, and only the root, has base_stats: every character starts with them", classesFile, id)
		case !root && len(c.StarterItems) > 0:
			return fmt.Errorf("%s: class %s: only the root has starter_items: every character starts with them", classesFile, id)
		case root == (c.AutoAssign != primaryYAML{}):
			return fmt.Errorf("%s: class %s: every class but the root has an auto_assign split; the root's players spend their own", classesFile, id)
		}
		if c.Look.Sprite == "" || c.Look.Tint != nil && (*c.Look.Tint < 0 || *c.Look.Tint > 0xffffff) || c.Look.Scale != nil {
			return fmt.Errorf("%s: class %s: look needs a sprite, a tint is an RGB color, and a class has no scale", classesFile, id)
		}
		kind := gamedata.AttackKind(c.Attack.Kind)
		switch kind {
		case gamedata.Melee:
			if c.Attack.Width <= 0 || c.Attack.Height <= 0 {
				return fmt.Errorf("%s: class %s: a melee attack needs width and height", classesFile, id)
			}
		case gamedata.Ranged:
			if c.Attack.Range <= 0 {
				return fmt.Errorf("%s: class %s: a ranged attack needs a range", classesFile, id)
			}
		default:
			return fmt.Errorf("%s: class %s: unknown attack kind %q", classesFile, id, c.Attack.Kind)
		}
		if c.Attack.Cooldown <= 0 {
			return fmt.Errorf("%s: class %s: cooldown must be positive", classesFile, id)
		}
		damage, err := parseDamage(c.Attack.Damage)
		if err != nil {
			return fmt.Errorf("%s: class %s: attack: %w", classesFile, id, err)
		}
		if c.AutoAssign.STR < 0 || c.AutoAssign.DEX < 0 || c.AutoAssign.INT < 0 || c.AutoAssign.AGI < 0 || c.AutoAssign.VIT < 0 {
			return fmt.Errorf("%s: class %s: auto_assign must not be negative", classesFile, id)
		}
		l.classes.add(id, gamedata.Class{
			ID: id, Name: c.Name, Parent: kernel.Class(c.Parent), Line: line,
			Attack:       gamedata.Attack{Kind: kind, Damage: damage, Width: c.Attack.Width, Height: c.Attack.Height, Range: c.Attack.Range, Cooldown: c.Attack.Cooldown},
			BaseStats:    gamedata.Stats(c.BaseStats),
			AutoAssign:   gamedata.Stats(c.AutoAssign),
			StarterItems: grants(c.StarterItems),
		})
	}
	return nil
}

// lineOf is the class at index i and each class above it: the first class is the root, and any other's parent comes earlier in the file, so the tree has no cycle.
func (l *loader) lineOf(i int, id, parent kernel.Class) ([]kernel.Class, error) {
	switch {
	case i == 0 && parent != "":
		return nil, fmt.Errorf("%s: class %s: the first class is the root, which every character starts as, and has no parent", classesFile, id)
	case i > 0 && parent == "":
		return nil, fmt.Errorf("%s: class %s: only the first class is the root; name a parent", classesFile, id)
	case i > 0 && !l.classes.has(parent):
		return nil, fmt.Errorf("%s: class %s: parent %s is not a class above it in the file", classesFile, id, parent)
	}
	line := []kernel.Class{id}
	if i > 0 {
		up, _ := l.classes.get(parent)
		line = append(line, up.Line...)
	}
	return line, nil
}
