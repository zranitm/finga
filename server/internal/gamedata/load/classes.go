package load

import (
	"errors"
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
	ID           string         `yaml:"id"`
	Name         string         `yaml:"name"`
	Description  string         `yaml:"description"`
	Parent       string         `yaml:"parent"`
	Look         lookYAML       `yaml:"look"`
	Attack       attackYAML     `yaml:"attack"`
	BaseStats    primaryYAML    `yaml:"base_stats"`
	AutoAssign   primaryYAML    `yaml:"auto_assign"`
	StarterItems []grantYAML    `yaml:"starter_items"`
	Skills       []string       `yaml:"skills"`
	Promotion    *promotionYAML `yaml:"promotion"`
}

type promotionYAML struct {
	Trainer string         `yaml:"trainer"`
	Level   int            `yaml:"level"`
	Stats   primaryYAML    `yaml:"stats"`
	Skills  map[string]int `yaml:"skills"`
	Quests  []string       `yaml:"quests"`
	Weapon  string         `yaml:"weapon"`
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
		if c.ID == "" || c.Name == "" || c.Description == "" {
			return fmt.Errorf("%s: class %d: needs an id, a name and a description", classesFile, i+1)
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
		case root == (c.Promotion != nil):
			return fmt.Errorf("%s: class %s: every class but the root has a promotion; the root is where every character starts", classesFile, id)
		}
		promotion, err := promotionOf(c.Promotion)
		if err != nil {
			return fmt.Errorf("%s: class %s: promotion: %w", classesFile, id, err)
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
			ID: id, Name: c.Name, Description: c.Description, Parent: kernel.Class(c.Parent), Line: line, Promotion: promotion,
			Attack:       gamedata.Attack{Kind: kind, Damage: damage, Width: c.Attack.Width, Height: c.Attack.Height, Range: c.Attack.Range, Cooldown: c.Attack.Cooldown},
			BaseStats:    gamedata.Stats(c.BaseStats),
			AutoAssign:   gamedata.Stats(c.AutoAssign),
			StarterItems: grants(c.StarterItems),
			Skills:       c.Skills,
		})
	}
	return nil
}

// promotionOf checks what a promotion asks on its own; checkPromotions holds its names to the files read after classes.yaml.
func promotionOf(raw *promotionYAML) (gamedata.Promotion, error) {
	if raw == nil {
		return gamedata.Promotion{}, nil
	}
	st := raw.Stats
	switch {
	case raw.Trainer == "" || raw.Weapon == "":
		return gamedata.Promotion{}, errors.New("needs a trainer and the weapon they hand over")
	case raw.Level < 1:
		return gamedata.Promotion{}, errors.New("level must be at least 1")
	case st.STR < 0 || st.DEX < 0 || st.INT < 0 || st.AGI < 0 || st.VIT < 0:
		return gamedata.Promotion{}, errors.New("stats must not be negative")
	}
	for id, points := range raw.Skills {
		if points < 1 {
			return gamedata.Promotion{}, fmt.Errorf("skill %s: at least 1 point", id)
		}
	}
	return gamedata.Promotion{Trainer: raw.Trainer, Level: raw.Level, Stats: gamedata.Stats(st), Skills: raw.Skills, Quests: raw.Quests, Weapon: raw.Weapon}, nil
}

// checkPromotions holds each promotion to the files read after classes.yaml: its trainer trains the class, its skills are the parent's to learn, its quests exist, and its weapon is the class's own, wearable at its level.
func (l *loader) checkPromotions(string) error {
	for _, c := range l.classes.list[1:] {
		p := c.Promotion
		parent, _ := l.classes.get(c.Parent)
		if n, ok := l.npcs.get(p.Trainer); !ok || n.Trainer != c.ID {
			return fmt.Errorf("%s: class %s: trainer %q is not an NPC in %s who trains it", classesFile, c.ID, p.Trainer, npcsFile)
		}
		for id := range p.Skills {
			if s, ok := l.skills.get(id); !ok || !parent.Learnable(s) {
				return fmt.Errorf("%s: class %s: promotion asks for skill %s, which a %s can't learn", classesFile, c.ID, id, parent.ID)
			}
		}
		for _, id := range p.Quests {
			if !l.quests.has(id) {
				return fmt.Errorf("%s: class %s: promotion asks for quest %s, which %s doesn't have", classesFile, c.ID, id, questsFile)
			}
		}
		if it, ok := l.items.get(p.Weapon); !ok || it.Slot != "weapon" || it.Class != c.ID || it.MinLevel > p.Level {
			return fmt.Errorf("%s: class %s: promotion weapon %q is not a %s weapon wearable at level %d", classesFile, c.ID, p.Weapon, c.ID, p.Level)
		}
	}
	for _, n := range l.npcs.list {
		if c, ok := l.classes.get(n.Trainer); ok && c.Promotion.Trainer != n.ID {
			return fmt.Errorf("%s: npc %s: trains %s, whose promotion names trainer %s", npcsFile, n.ID, c.ID, c.Promotion.Trainer)
		}
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
