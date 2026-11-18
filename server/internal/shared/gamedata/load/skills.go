package load

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"time"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/kernel"
)

const skillsFile = "skills.yaml"

// iconPath is a game-icons.net path as its site lists them: author, then name.
var iconPath = regexp.MustCompile(`^[a-z0-9-]+/[a-z0-9-]+$`)

type skillsYAML struct {
	Skills []skillYAML `yaml:"skills"`
}

type skillYAML struct {
	ID            string      `yaml:"id"`
	Kind          string      `yaml:"kind"`
	Effect        string      `yaml:"effect"`
	Level         int         `yaml:"level"`
	BreaksControl bool        `yaml:"breaks_control"`
	Granted       bool        `yaml:"granted"`
	Stages        []stageYAML `yaml:"stages"`
	skillFields   `yaml:",inline"`
}

type stageYAML struct {
	At          int    `yaml:"at"`
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	skillFields `yaml:",inline"`
}

// skillFields are what a skill sets and a stage may change; a stage leaves alone what it doesn't name.
type skillFields struct {
	Icon     string                `yaml:"icon"`
	Damage   string                `yaml:"damage"`
	Shape    *shapeYAML            `yaml:"shape"`
	Energy   int                   `yaml:"energy"`
	Cooldown time.Duration         `yaml:"cooldown"`
	Power    *scaledYAML           `yaml:"power"`
	Targets  int                   `yaml:"targets"`
	Duration time.Duration         `yaml:"duration"`
	Bonus    map[string]scaledYAML `yaml:"bonus"`

	Move       *moveYAML     `yaml:"move"`
	Dodge      time.Duration `yaml:"dodge"`
	Status     *statusYAML   `yaml:"status"`
	Push       *pushYAML     `yaml:"push"`
	Hits       *hitsYAML     `yaml:"hits"`
	ReachParty *bool         `yaml:"reach_party"`
	OnHit      *onHitYAML    `yaml:"on_hit"`
	Shield     *shieldYAML   `yaml:"shield"`

	timingYAML `yaml:",inline"`
	Cancelable *bool `yaml:"cancelable"`
}

func (f skillFields) timed() bool { return f.named() || f.Cancelable != nil }

type shapeYAML struct {
	Kind   string  `yaml:"kind"`
	Radius float64 `yaml:"radius"`
	Angle  float64 `yaml:"angle"`
	Height float64 `yaml:"height"`
	Range  float64 `yaml:"range"`
	Width  float64 `yaml:"width"`
	At     string  `yaml:"at"`
	Reach  float64 `yaml:"reach"`
}

func (s shapeYAML) shape() gamedata.Shape {
	return gamedata.Shape{Kind: gamedata.ShapeKind(s.Kind), Radius: s.Radius, Angle: s.Angle, Height: s.Height, Range: s.Range, Width: s.Width, At: gamedata.ShapeAt(s.At), Reach: s.Reach}
}

// moveYAML, statusYAML, onHitYAML, hitsYAML and shieldYAML change only what they name; a new kind of move, or a new status, starts afresh.
type moveYAML struct {
	Kind     string        `yaml:"kind"`
	Distance float64       `yaml:"distance"`
	Time     time.Duration `yaml:"time"`
	Toward   string        `yaml:"toward"`
	Stop     *bool         `yaml:"stop"`
	Through  *bool         `yaml:"through"`
}

func (raw moveYAML) over(m gamedata.Move) (gamedata.Move, error) {
	if k := gamedata.MoveKind(raw.Kind); k != "" && k != m.Kind {
		m = gamedata.Move{Kind: k, Stop: k != gamedata.BlinkMove}
	}
	if raw.Distance != 0 {
		m.Distance = raw.Distance
	}
	if raw.Time != 0 {
		m.Time = raw.Time
	}
	if raw.Toward != "" {
		m.Toward = gamedata.Toward(raw.Toward)
	}
	if raw.Stop != nil {
		if m.Kind == gamedata.BlinkMove {
			return m, errors.New("a blink names no stop: it tests every 0.25 m of its way")
		}
		m.Stop = *raw.Stop
	}
	if raw.Through != nil {
		if m.Kind == gamedata.BlinkMove {
			return m, errors.New("a blink names no through: it passes mobs already")
		}
		if raw.Stop != nil && *raw.Stop && *raw.Through {
			return m, errors.New("a move that stops at a mob can't pass through them")
		}
		m.Through = *raw.Through
		m.Stop = m.Stop && !m.Through
	}
	return m, nil
}

// statusYAML is a skill's status on what it lands on; onHitYAML a buff's on each of its user's hits, as statuses.md names their keys.
type statusYAML struct {
	ID    string        `yaml:"id"`
	Time  time.Duration `yaml:"time"`
	Power *scaledYAML   `yaml:"power"`
}

type onHitYAML struct {
	Status string        `yaml:"status"`
	Time   time.Duration `yaml:"time"`
	Power  *scaledYAML   `yaml:"power"`
}

// inflictOver is in with what a status names over it: a new status starts afresh.
func inflictOver(in gamedata.SkillInflict, status string, t time.Duration, power *scaledYAML) gamedata.SkillInflict {
	if status != "" && status != in.Status {
		in = gamedata.SkillInflict{Status: status}
	}
	if t != 0 {
		in.Time = t
	}
	if power != nil {
		in.Power = power.over(in.Power)
	}
	return in
}

type pushYAML struct {
	Distance float64 `yaml:"distance"`
}

type hitsYAML struct {
	Count int           `yaml:"count"`
	Every time.Duration `yaml:"every"`
}

func (raw hitsYAML) over(h gamedata.Hits) gamedata.Hits {
	if raw.Count != 0 {
		h.Count = raw.Count
	}
	if raw.Every != 0 {
		h.Every = raw.Every
	}
	return h
}

type shieldYAML struct {
	Power *scaledYAML `yaml:"power"`
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
		s, err := l.skillOf(raw)
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

// skillOf resolves every stage: the skill's own fields, then each stage's changes in turn; an active skill's cancel and ready points are actions.yaml's unless it names its own, and its cost at most the energy a player holds.
func (l *loader) skillOf(raw skillYAML) (gamedata.Skill, error) {
	s := gamedata.Skill{ID: raw.ID, Kind: gamedata.SkillKind(raw.Kind), Effect: gamedata.SkillEffect(raw.Effect), Level: max(raw.Level, 1), BreaksControl: raw.BreaksControl, Granted: raw.Granted}
	switch {
	case s.Kind == gamedata.Active && slices.Contains([]gamedata.SkillEffect{gamedata.DamageEffect, gamedata.HealEffect, gamedata.BuffEffect, gamedata.StatusEffect, gamedata.MoveEffect}, s.Effect):
	case s.Kind == gamedata.Passive && s.Effect == gamedata.BonusEffect:
	default:
		return s, fmt.Errorf("kind %q with effect %q: an active skill does damage, heal, buff, status or move, a passive one bonus", raw.Kind, raw.Effect)
	}
	if raw.Level < 0 {
		return s, fmt.Errorf("level %d: from 1 to the level cap", raw.Level)
	}
	if raw.Icon == "" {
		return s, errors.New("needs an icon, a game-icons.net path such as lorc/broadsword")
	}
	if len(raw.Stages) == 0 || raw.Stages[0].At != 0 {
		return s, errors.New("the first stage is at 0 points")
	}
	switch {
	case s.BreaksControl && (!s.Granted || s.Kind != gamedata.Active):
		return s, errors.New("a break-free skill is granted and active: anyone held can use it")
	case s.Granted && len(raw.Stages) > 1:
		return s, errors.New("a granted skill takes no points, so it has one stage")
	}
	cur := gamedata.Stage{Targets: 1}
	if s.Kind == gamedata.Active {
		cur.Timing = l.shares
	} else if raw.timed() || slices.ContainsFunc(raw.Stages, func(st stageYAML) bool { return st.timed() }) {
		return s, errors.New("a passive skill has no length, hit, cancel, ready or cancelable: it is never used")
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
		if err := l.check(s, cur); err != nil {
			return s, fmt.Errorf("stage %s: %w", st.Name, err)
		}
		s.Stages = append(s.Stages, cur)
	}
	return s, nil
}

// apply changes st by what f names.
func apply(st *gamedata.Stage, f skillFields) error {
	if f.Icon != "" {
		if !iconPath.MatchString(f.Icon) {
			return fmt.Errorf("icon %q: a game-icons.net path, author/name, such as lorc/broadsword", f.Icon)
		}
		st.Icon = f.Icon
	}
	if f.Damage != "" {
		d, err := parseDamage(f.Damage)
		if err != nil {
			return err
		}
		st.Damage = d
	}
	if f.Shape != nil {
		st.Shape = f.Shape.shape()
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
	if f.Move != nil {
		m, err := f.Move.over(st.Move)
		if err != nil {
			return err
		}
		st.Move = m
	}
	if f.Dodge != 0 {
		st.Dodge = f.Dodge
	}
	if f.Status != nil {
		st.Status = inflictOver(st.Status, f.Status.ID, f.Status.Time, f.Status.Power)
	}
	if f.Push != nil {
		st.Push = f.Push.Distance
	}
	if f.Hits != nil {
		st.Hits = f.Hits.over(st.Hits)
	}
	if f.ReachParty != nil {
		st.ReachParty = *f.ReachParty
	}
	if f.OnHit != nil {
		st.OnHit = inflictOver(st.OnHit, f.OnHit.Status, f.OnHit.Time, f.OnHit.Power)
	}
	if f.Shield != nil && f.Shield.Power != nil {
		st.Shield.Power = f.Shield.Power.over(st.Shield.Power)
	}
	return nil
}

// check holds a resolved stage to what its skill's kind and effect need, and refuses what they don't have.
func (l *loader) check(s gamedata.Skill, st gamedata.Stage) error {
	maxEnergy := l.energy.Max
	active := s.Kind == gamedata.Active
	effect := func(e ...gamedata.SkillEffect) bool { return slices.Contains(e, s.Effect) }
	hits := effect(gamedata.DamageEffect, gamedata.HealEffect)
	noBonus := st.Bonus == gamedata.StatSet[gamedata.Scaled]{}
	switch {
	case active != (st.Cooldown > 0):
		return errors.New("an active skill, and only an active one, has a cooldown")
	case s.BreaksControl && st.Energy != 0:
		return errors.New("a break-free skill costs no energy: a player held at 0 must still get out")
	case active && !s.BreaksControl && (st.Energy < 1 || st.Energy > maxEnergy):
		return fmt.Errorf("an active skill costs energy from 1 to %d, the most a player holds; only a break-free skill costs none", maxEnergy)
	case !active && (st.Energy != 0 || st.Shape != gamedata.Shape{}):
		return errors.New("a passive skill has no energy or shape")
	case !active && (st.Move != gamedata.Move{} || st.Dodge != 0):
		return errors.New("a passive skill has no move or dodge: it is never used")
	case hits != (st.Power != gamedata.Scaled{}):
		return errors.New("a damage or heal skill, and only one, has power")
	case hits && (!positive(st.Power.Base) || !nonNegative(st.Power.PerPoint)):
		return errors.New("power must be positive and not fall with points")
	case hits && st.Targets < 1:
		return errors.New("targets must be at least 1")
	case !hits && st.Targets != 1:
		return errors.New("only a damage or heal skill has targets")
	case effect(gamedata.DamageEffect) != (st.Damage != ""):
		return errors.New("a damage skill, and only one, has a damage type")
	case effect(gamedata.HealEffect) && st.Shape.Kind != gamedata.AreaShape:
		return errors.New("a heal reaches an area round its caster")
	case effect(gamedata.BuffEffect) != (st.Duration > 0):
		return errors.New("a buff, and only a buff, has a duration")
	case effect(gamedata.BonusEffect) && noBonus:
		return errors.New("a passive skill has a bonus")
	case effect(gamedata.BuffEffect) && noBonus && st.OnHit == gamedata.SkillInflict{} && st.Shield == gamedata.Shield{}:
		return errors.New("a buff gives a bonus, an on_hit or a shield")
	case !effect(gamedata.BuffEffect, gamedata.BonusEffect) && !noBonus:
		return errors.New("only a buff or a passive skill has a bonus")
	case !effect(gamedata.BuffEffect) && (st.OnHit != gamedata.SkillInflict{} || st.Shield != gamedata.Shield{} || st.ReachParty):
		return errors.New("only a buff has an on_hit, a shield or reach_party")
	case st.ReachParty && st.Shape.Kind != gamedata.AreaShape:
		return errors.New("reach_party reaches the members within an area's radius")
	case st.ReachParty && st.Bonus.MoveSpeed != (gamedata.Scaled{}):
		return errors.New("a party buff gives no move speed: a member's client predicts its own walk, not another's buff")
	case effect(gamedata.StatusEffect) && st.Status == gamedata.SkillInflict{} && st.Push == 0:
		return errors.New("a status skill has a status or a push")
	case !effect(gamedata.DamageEffect, gamedata.StatusEffect) && (st.Status != gamedata.SkillInflict{} || st.Push != 0):
		return errors.New("only a damage or status skill has a status or a push")
	case st.Push < 0 || !finite(st.Push):
		return errors.New("a push is a distance above 0")
	case effect(gamedata.MoveEffect) && (st.Move == gamedata.Move{}) && !s.BreaksControl:
		return errors.New("a move skill has a move; a break-free one may only end the hold and dodge")
	case effect(gamedata.MoveEffect) && (st.Shape != gamedata.Shape{}):
		return errors.New("a move skill has no shape: it hits nothing")
	case !effect(gamedata.DamageEffect) && (st.Hits != gamedata.Hits{}):
		return errors.New("only a damage skill repeats its hit")
	case st.Dodge < 0:
		return errors.New("a dodge is a time from the action's start, not below 0")
	}
	if active && !effect(gamedata.MoveEffect) {
		if err := checkShape(st.Shape); err != nil {
			return err
		}
	}
	if active {
		if err := checkTiming(st.Timing); err != nil {
			return err
		}
	}
	for _, err := range []error{checkMove(st.Move), checkHits(st.Hits), checkShield(st.Shield)} {
		if err != nil {
			return err
		}
	}
	for _, in := range []struct {
		key string
		in  gamedata.SkillInflict
	}{{"status", st.Status}, {"on_hit", st.OnHit}} {
		if in.in == (gamedata.SkillInflict{}) {
			continue
		}
		if err := l.inflictOf(in.in); err != nil {
			return fmt.Errorf("%s: %w", in.key, err)
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
	case gamedata.ArcShape:
		if !positive(s.Radius) || !(s.Angle > 0 && s.Angle <= 360) || !positive(s.Height) || s.Range != 0 || s.Width != 0 || s.At != gamedata.AtUser || s.Reach != 0 {
			return errors.New("an arc has a radius, an angle of up to 360 and a height, and nothing else")
		}
	case gamedata.ShotShape:
		if !positive(s.Range) || !positive(s.Width) || s.Radius != 0 || s.Angle != 0 || s.Height != 0 || s.At != gamedata.AtUser || s.Reach != 0 {
			return errors.New("a shot has a range and a width, and nothing else")
		}
	case gamedata.AreaShape:
		if !positive(s.Radius) || s.Angle != 0 || s.Height != 0 || s.Range != 0 || s.Width != 0 {
			return errors.New("an area has a radius, and nothing else but at and reach")
		}
		switch {
		case s.At == gamedata.AtUser && s.Reach != 0:
			return errors.New("an area's reach goes with at: lock")
		case s.At == gamedata.AtLock && !positive(s.Reach):
			return errors.New("an area at the lock has a reach, how far off its middle may be")
		case s.At != gamedata.AtUser && s.At != gamedata.AtLock:
			return fmt.Errorf("an area at %q: at the user, or at lock", s.At)
		}
	default:
		return fmt.Errorf("shape kind %q: arc, shot or area", s.Kind)
	}
	return nil
}

// checkMove holds a move, if any, to its kind: a distance and a way, and a dash's or leap's time; a blink has none.
func checkMove(m gamedata.Move) error {
	if m == (gamedata.Move{}) {
		return nil
	}
	switch {
	case !slices.Contains([]gamedata.MoveKind{gamedata.DashMove, gamedata.LeapMove, gamedata.BlinkMove}, m.Kind):
		return fmt.Errorf("move kind %q: dash, leap or blink", m.Kind)
	case !positive(m.Distance):
		return errors.New("a move has a distance")
	case m.Kind != gamedata.BlinkMove && m.Time <= 0:
		return fmt.Errorf("a %s has a time", m.Kind)
	case m.Kind == gamedata.BlinkMove && m.Time != 0:
		return errors.New("a blink has no time: it moves at once")
	case !slices.Contains([]gamedata.Toward{gamedata.TowardLock, gamedata.TowardBack, gamedata.TowardHeading}, m.Toward):
		return fmt.Errorf("move toward %q: lock, back or heading", m.Toward)
	}
	return nil
}

func checkHits(h gamedata.Hits) error {
	if h != (gamedata.Hits{}) && (h.Count < 2 || h.Every <= 0) {
		return errors.New("hits repeat at least twice, a time apart")
	}
	return nil
}

func checkShield(s gamedata.Shield) error {
	if s != (gamedata.Shield{}) && (!positive(s.Power.Base) || !nonNegative(s.Power.PerPoint)) {
		return errors.New("a shield's power is positive and doesn't fall with points")
	}
	return nil
}

// checkSkillLevels holds each skill's level to the level cap, which exp_curve.yaml gives after skills.yaml.
func (l *loader) checkSkillLevels(string) error {
	for _, s := range l.skills.list {
		if s.Level > l.levelCap {
			return fmt.Errorf("%s: skill %s: level %d: from 1 to the level cap, %d", skillsFile, s.ID, s.Level, l.levelCap)
		}
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
