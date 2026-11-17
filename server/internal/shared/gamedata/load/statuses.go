package load

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"finga/internal/shared/gamedata"
)

const statusesFile = "statuses.yaml"

type statusesYAML struct {
	HardImmunity time.Duration   `yaml:"hard_immunity"`
	SuperArmor   superArmorYAML  `yaml:"super_armor"`
	Statuses     []statusDefYAML `yaml:"statuses"`
}

type superArmorYAML struct {
	Hard   string  `yaml:"hard"`
	Others float64 `yaml:"others"`
}

type statusDefYAML struct {
	ID              string        `yaml:"id"`
	Group           string        `yaml:"group"`
	Blocks          []string      `yaml:"blocks"`
	MoveSpeed       *float64      `yaml:"move_speed"`
	Accuracy        *float64      `yaml:"accuracy"`
	Attack          *float64      `yaml:"attack"`
	PhysicalDefense *float64      `yaml:"physical_defense"`
	MagicDefense    *float64      `yaml:"magic_defense"`
	Shatter         float64       `yaml:"shatter"`
	Wakes           bool          `yaml:"wakes"`
	Every           time.Duration `yaml:"every"`
	Moving          *float64      `yaml:"moving"`
}

var (
	statusGroups = []gamedata.StatusGroup{gamedata.Hard, gamedata.Soft, gamedata.Dot, gamedata.Weaken, gamedata.Aggro}
	blockNames   = map[string]gamedata.Blocks{"move": gamedata.BlocksMove, "act": gamedata.BlocksAct, "skills": gamedata.BlocksSkills}
)

// loadStatuses reads statuses.yaml before the skills and mobs that name its statuses.
func (l *loader) loadStatuses(dir string) error {
	var file statusesYAML
	if err := readYAML(dir, statusesFile, &file); err != nil {
		return err
	}
	switch {
	case file.HardImmunity < 0:
		return fmt.Errorf("%s: hard_immunity %s: not below 0", statusesFile, file.HardImmunity)
	case file.SuperArmor.Hard != "immune":
		return fmt.Errorf("%s: super_armor hard %q: super armor is immune to hard statuses", statusesFile, file.SuperArmor.Hard)
	case !positive(file.SuperArmor.Others) || file.SuperArmor.Others > 1:
		return fmt.Errorf("%s: super_armor others %g: a share of the time, above 0 and at most 1", statusesFile, file.SuperArmor.Others)
	}
	l.statusRules = gamedata.StatusRules{HardImmunity: file.HardImmunity, SuperArmor: file.SuperArmor.Others}
	for i, raw := range file.Statuses {
		if raw.ID == "" {
			return fmt.Errorf("%s: status %d: needs an id", statusesFile, i+1)
		}
		if l.statuses.has(raw.ID) {
			return fmt.Errorf("%s: duplicate status %s", statusesFile, raw.ID)
		}
		s, err := statusOf(raw)
		if err != nil {
			return fmt.Errorf("%s: status %s: %w", statusesFile, raw.ID, err)
		}
		l.statuses.add(s.ID, s)
	}
	if s, ok := l.statuses.get(gamedata.Knockdown); !ok || s.Group != gamedata.Hard || !s.Blocks.Has(gamedata.BlocksMove|gamedata.BlocksAct) {
		return fmt.Errorf("%s: needs a knockdown, a hard status that blocks move and act: the down pose", statusesFile)
	}
	return nil
}

// statusOf checks one status: a known group and blocks, positive factors, and a tick only for damage over time.
func statusOf(raw statusDefYAML) (gamedata.Status, error) {
	s := gamedata.Status{ID: raw.ID, Group: gamedata.StatusGroup(raw.Group), Shatter: raw.Shatter, Wakes: raw.Wakes, Every: raw.Every}
	if !slices.Contains(statusGroups, s.Group) {
		return s, fmt.Errorf("group %q: hard, soft, dot, weaken or aggro", raw.Group)
	}
	for _, name := range raw.Blocks {
		b, ok := blockNames[name]
		if !ok || s.Blocks.Has(b) {
			return s, fmt.Errorf("blocks %q: each of move, act and skills, once", name)
		}
		s.Blocks |= b
	}
	for _, f := range []struct {
		name string
		raw  *float64
		into *float64
	}{{"move_speed", raw.MoveSpeed, &s.MoveSpeed}, {"accuracy", raw.Accuracy, &s.Accuracy}, {"attack", raw.Attack, &s.Attack}, {"physical_defense", raw.PhysicalDefense, &s.PhysicalDefense}, {"magic_defense", raw.MagicDefense, &s.MagicDefense}, {"moving", raw.Moving, &s.Moving}} {
		*f.into = 1
		if f.raw == nil {
			continue
		}
		if !positive(*f.raw) {
			return s, fmt.Errorf("%s %g: a factor above 0", f.name, *f.raw)
		}
		*f.into = *f.raw
	}
	dot := s.Group == gamedata.Dot
	switch {
	case s.Shatter != 0 && !positive(s.Shatter):
		return s, errors.New("shatter: a factor above 0 on the hit that ends it")
	case dot != (s.Every > 0):
		return s, errors.New("damage over time, and only it, ticks every a time above 0")
	case !dot && raw.Moving != nil:
		return s, errors.New("only damage over time has moving")
	case s.Group == gamedata.Aggro && s.ID != gamedata.Taunt && s.ID != gamedata.LoseSight:
		return s, errors.New("an aggro status is taunt or lose_sight, which the mobs know")
	}
	return s, nil
}

// inflictOf checks what a skill or a mob's attack inflicts, its power at 0 points: a known status, a time above 0 but for a knockdown's, which lasts the down pose, and power on damage over time alone.
func (l *loader) inflictOf(in gamedata.SkillInflict) error {
	s, ok := l.statuses.get(in.Status)
	switch {
	case !ok:
		return fmt.Errorf("status %q: not in %s", in.Status, statusesFile)
	case in.Status == gamedata.Knockdown && in.Time != 0:
		return errors.New("a knockdown names no time: it lasts the down pose")
	case in.Status != gamedata.Knockdown && in.Time <= 0:
		return fmt.Errorf("a %s lasts a time above 0", in.Status)
	case s.Group == gamedata.Dot && (!positive(in.Power.Base) || !nonNegative(in.Power.PerPoint)):
		return fmt.Errorf("a %s has power, positive and not falling with points", in.Status)
	case s.Group != gamedata.Dot && in.Power != (gamedata.Scaled{}):
		return fmt.Errorf("a %s has no power: only damage over time does", in.Status)
	}
	return nil
}
