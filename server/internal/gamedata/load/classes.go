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
	for _, c := range file.Classes {
		id, err := kernel.ParseClass(c.ID)
		if err != nil {
			return fmt.Errorf("%s: class %q: not a class", classesFile, c.ID)
		}
		if l.classes.has(id) {
			return fmt.Errorf("%s: duplicate class %s", classesFile, id)
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
			ID: id, Name: c.Name,
			Attack:       gamedata.Attack{Kind: kind, Damage: damage, Width: c.Attack.Width, Height: c.Attack.Height, Range: c.Attack.Range, Cooldown: c.Attack.Cooldown},
			BaseStats:    gamedata.Stats(c.BaseStats),
			AutoAssign:   gamedata.Stats(c.AutoAssign),
			StarterItems: grants(c.StarterItems),
		})
	}
	return nil
}
