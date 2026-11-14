package load

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"finga/internal/gamedata"
	"finga/internal/kernel"
)

const skillsFile = "skills.yaml"

type skillsYAML struct {
	Skills []skillYAML `yaml:"skills"`
}

type skillYAML struct {
	ID          string      `yaml:"id"`
	Kind        string      `yaml:"kind"`
	Effect      string      `yaml:"effect"`
	Stages      []stageYAML `yaml:"stages"`
	skillFields `yaml:",inline"`
}

type stageYAML struct {
	At          int    `yaml:"at"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	skillFields `yaml:",inline"`
}

// skillFields are what a skill sets and a stage may change; a stage leaves alone what it doesn't name.
type skillFields struct {
	Damage   string                `yaml:"damage"`
	Shape    *shapeYAML            `yaml:"shape"`
	Energy   int                   `yaml:"energy"`
	Cooldown time.Duration         `yaml:"cooldown"`
	Power    *scaledYAML           `yaml:"power"`
	Targets  int                   `yaml:"targets"`
	Duration time.Duration         `yaml:"duration"`
	Bonus    map[string]scaledYAML `yaml:"bonus"`

	timingYAML `yaml:",inline"`
	Cancelable *bool `yaml:"cancelable"`
}

func (f skillFields) timed() bool { return f.named() || f.Cancelable != nil }

type shapeYAML struct {
	Kind   string  `yaml:"kind"`
	Width  float64 `yaml:"width"`
	Height float64 `yaml:"height"`
	Range  float64 `yaml:"range"`
	Radius float64 `yaml:"radius"`
}

type scaledYAML struct {
	Base     *float64 `yaml:"base"`
	PerPoint *float64 `yaml:"per_point"`
}

// over is s with what raw names.
func (raw scaledYAML) over(s gamedata.Scaled) gamedata.Scaled {
	if raw.Base != nil {
		s.Base = *raw.Base
	}
	if raw.PerPoint != nil {
		s.PerPoint = *raw.PerPoint
	}
	return s
}

// loadSkills reads skills.yaml after classes.yaml, whose classes list the skills they teach: each skill is taught by exactly one class.
func (l *loader) loadSkills(dir string) error {
	var file skillsYAML
	if err := readYAML(dir, skillsFile, &file); err != nil {
		return err
	}
	teacher := map[string]kernel.Class{}
	for _, c := range l.classes.list {
		for _, id := range c.Skills {
			if other, ok := teacher[id]; ok {
				return fmt.Errorf("%s: skill %s is taught by both %s and %s", classesFile, id, other, c.ID)
			}
			teacher[id] = c.ID
		}
	}
	for i, raw := range file.Skills {
		if raw.ID == "" {
			return fmt.Errorf("%s: skill %d: needs an id", skillsFile, i+1)
		}
		if l.skills.has(raw.ID) {
			return fmt.Errorf("%s: duplicate skill %s", skillsFile, raw.ID)
		}
		class, ok := teacher[raw.ID]
		if !ok {
			return fmt.Errorf("%s: skill %s: no class in %s teaches it", skillsFile, raw.ID, classesFile)
		}
		s, err := skillOf(raw, l.cancel, l.energy.Max)
		if err != nil {
			return fmt.Errorf("%s: skill %s: %w", skillsFile, raw.ID, err)
		}
		s.Class = class
		l.skills.add(s.ID, s)
	}
	for _, id := range slices.Sorted(maps.Keys(teacher)) {
		if !l.skills.has(id) {
			return fmt.Errorf("%s: class %s teaches skill %s, which %s doesn't have", classesFile, teacher[id], id, skillsFile)
		}
	}
	return nil
}

// skillOf resolves every stage: the skill's own fields, then each stage's changes in turn; an active skill's cancel point is cancel unless it names its own, and its cost at most maxEnergy.
func skillOf(raw skillYAML, cancel float64, maxEnergy int) (gamedata.Skill, error) {
	s := gamedata.Skill{ID: raw.ID, Kind: gamedata.SkillKind(raw.Kind), Effect: gamedata.SkillEffect(raw.Effect)}
	switch {
	case s.Kind == gamedata.Active && (s.Effect == gamedata.DamageEffect || s.Effect == gamedata.HealEffect || s.Effect == gamedata.BuffEffect):
	case s.Kind == gamedata.Passive && s.Effect == gamedata.BonusEffect:
	default:
		return s, fmt.Errorf("kind %q with effect %q: an active skill does damage, heal or buff, a passive one bonus", raw.Kind, raw.Effect)
	}
	if len(raw.Stages) == 0 || raw.Stages[0].At != 0 {
		return s, errors.New("the first stage is at 0 points")
	}
	cur := gamedata.Stage{Targets: 1}
	if s.Kind == gamedata.Active {
		cur.Cancel = cancel
	} else if raw.timed() || slices.ContainsFunc(raw.Stages, func(st stageYAML) bool { return st.timed() }) {
		return s, errors.New("a passive skill has no length, hit, cancel or cancelable: it is never used")
	}
	if err := apply(&cur, raw.skillFields); err != nil {
		return s, err
	}
	for k, st := range raw.Stages {
		if k > 0 && st.At <= raw.Stages[k-1].At {
			return s, fmt.Errorf("stage %s: at %d, but the totals must rise", st.Name, st.At)
		}
		if st.Name == "" || st.Description == "" {
			return s, fmt.Errorf("stage %d: needs a name and a description", k+1)
		}
		if err := apply(&cur, st.skillFields); err != nil {
			return s, fmt.Errorf("stage %s: %w", st.Name, err)
		}
		cur.At, cur.Name, cur.Description = st.At, st.Name, st.Description
		if err := check(s, cur, maxEnergy); err != nil {
			return s, fmt.Errorf("stage %s: %w", st.Name, err)
		}
		s.Stages = append(s.Stages, cur)
	}
	return s, nil
}

// apply changes st by what f names.
func apply(st *gamedata.Stage, f skillFields) error {
	if f.Damage != "" {
		d, err := parseDamage(f.Damage)
		if err != nil {
			return err
		}
		st.Damage = d
	}
	if f.Shape != nil {
		st.Shape = gamedata.Shape{Kind: gamedata.ShapeKind(f.Shape.Kind), Width: f.Shape.Width, Height: f.Shape.Height, Range: f.Shape.Range, Radius: f.Shape.Radius}
	}
	if f.Energy != 0 {
		st.Energy = f.Energy
	}
	if f.Cooldown != 0 {
		st.Cooldown = f.Cooldown
	}
	if f.Power != nil {
		st.Power = f.Power.over(st.Power)
	}
	if f.Targets != 0 {
		st.Targets = f.Targets
	}
	if f.Duration != 0 {
		st.Duration = f.Duration
	}
	st.Timing = f.over(st.Timing)
	if f.Cancelable != nil {
		st.Cancelable = *f.Cancelable
	}
	for _, name := range slices.Sorted(maps.Keys(f.Bonus)) {
		field := setField(&st.Bonus, name)
		if field == nil {
			return fmt.Errorf("bonus to unknown stat %s", name)
		}
		*field = f.Bonus[name].over(*field)
	}
	return nil
}

// check holds a resolved stage to what its skill's kind and effect need, and refuses what they don't have.
func check(s gamedata.Skill, st gamedata.Stage, maxEnergy int) error {
	active := s.Kind == gamedata.Active
	hits := s.Effect == gamedata.DamageEffect || s.Effect == gamedata.HealEffect
	gives := s.Effect == gamedata.BuffEffect || s.Effect == gamedata.BonusEffect
	switch {
	case active != (st.Cooldown > 0):
		return errors.New("an active skill, and only an active one, has a cooldown")
	case active && (st.Energy < 1 || st.Energy > maxEnergy):
		return fmt.Errorf("an active skill costs energy from 1 to %d, the most a player holds", maxEnergy)
	case !active && (st.Energy != 0 || st.Shape != gamedata.Shape{}):
		return errors.New("a passive skill has no energy or shape")
	case hits != (st.Power != gamedata.Scaled{}):
		return errors.New("a damage or heal skill, and only one, has power")
	case hits && (!positive(st.Power.Base) || !nonNegative(st.Power.PerPoint)):
		return errors.New("power must be positive and not fall with points")
	case hits && st.Targets < 1:
		return errors.New("targets must be at least 1")
	case !hits && st.Targets != 1:
		return errors.New("only a damage or heal skill has targets")
	case (s.Effect == gamedata.DamageEffect) != (st.Damage != ""):
		return errors.New("a damage skill, and only one, has a damage type")
	case (s.Effect == gamedata.BuffEffect) != (st.Duration > 0):
		return errors.New("a buff, and only a buff, has a duration")
	case gives == (st.Bonus == gamedata.StatSet[gamedata.Scaled]{}):
		return errors.New("a buff or passive bonus, and only one, has a bonus")
	}
	if active {
		if err := checkShape(st.Shape); err != nil {
			return err
		}
		if err := checkTiming(st.Timing); err != nil {
			return err
		}
	}
	for _, v := range bonusValues(st.Bonus) {
		if !nonNegative(v.Base) || !nonNegative(v.PerPoint) {
			return errors.New("a bonus never takes away")
		}
	}
	return nil
}

func checkShape(s gamedata.Shape) error {
	switch s.Kind {
	case gamedata.MeleeShape:
		if !positive(s.Width) || !positive(s.Height) || s.Range != 0 || s.Radius != 0 {
			return errors.New("a melee shape has a width and a height, and nothing else")
		}
	case gamedata.RangedShape:
		if !positive(s.Range) || s.Width != 0 || s.Height != 0 || s.Radius != 0 {
			return errors.New("a ranged shape has a range, and nothing else")
		}
	case gamedata.AreaShape:
		if !positive(s.Radius) || s.Width != 0 || s.Height != 0 || s.Range != 0 {
			return errors.New("an area shape has a radius, and nothing else")
		}
	default:
		return fmt.Errorf("shape kind %q: melee, ranged or area", s.Kind)
	}
	return nil
}

// bonusValues are a bonus's 18 values.
func bonusValues(b gamedata.StatSet[gamedata.Scaled]) []gamedata.Scaled {
	return []gamedata.Scaled{
		b.STR, b.DEX, b.INT, b.AGI, b.VIT,
		b.PhysicalAttack, b.MagicAttack, b.PhysicalDefense, b.MagicDefense,
		b.MaxHP, b.Accuracy, b.Evasion,
		b.CritRate, b.CritDamage, b.Penetration, b.HPRegen,
		b.CooldownSpeed, b.MoveSpeed,
	}
}
