package gamedata

import (
	"time"

	"finga/internal/shared/kernel"
)

// SkillKind is whether a skill is used from the skill bar or always on.
type SkillKind string

// The skill kinds.
const (
	Active  SkillKind = "active"
	Passive SkillKind = "passive"
)

// SkillEffect is what a skill does: an active one damages, heals or buffs; a passive one adds a bonus.
type SkillEffect string

// The skill effects.
const (
	DamageEffect SkillEffect = "damage"
	HealEffect   SkillEffect = "heal"
	BuffEffect   SkillEffect = "buff"
	BonusEffect  SkillEffect = "bonus"
)

// ShapeKind is where an action finds its targets, from the user's feet centre toward its aim.
type ShapeKind string

// The shape kinds.
const (
	ArcShape  ShapeKind = "arc"  // the sector of Radius and Angle about the aim, up to Height above the feet
	ShotShape ShapeKind = "shot" // a lane Width wide along the aim, up to Range
	AreaShape ShapeKind = "area" // within Radius of the user
)

// Shape is where an action finds its targets: lengths in metres, the angle in degrees.
type Shape struct {
	Kind                                ShapeKind
	Radius, Angle, Height, Range, Width float64
}

// Scaled grows with the points invested in a skill.
type Scaled struct {
	Base, PerPoint float64
}

// At is the value with points invested.
func (s Scaled) At(points int) float64 { return s.Base + s.PerPoint*float64(points) }

// Stage is a skill from At points on, every field resolved from the stages before it.
type Stage struct {
	At          int
	Name        string
	Description string
	Damage      DamageType      // damage
	Shape       Shape           // active
	Energy      int             // active: what using it costs
	Cooldown    time.Duration   // active
	Timing                      // active
	Cancelable  bool            // active: a basic attack may cut it off from its cancel point
	Power       Scaled          // damage and heal: times the matching attack
	Targets     int             // damage and heal: at most this many in the shape
	Duration    time.Duration   // buff
	Bonus       StatSet[Scaled] // bonus and buff, in the units items use
}

// Skill is a skill and its evolutions, in data/skills.yaml.
type Skill struct {
	ID     string
	Kind   SkillKind
	Effect SkillEffect
	Class  kernel.Class // the class that teaches it
	Stages []Stage      // the first at 0 points, the totals rising
}

// StageAt is the index of the stage a skill is in with points invested: the last whose total they reach.
func (s Skill) StageAt(points int) int {
	k := 0
	for i, st := range s.Stages {
		if points >= st.At {
			k = i
		}
	}
	return k
}

// Skill is the skill with id.
func (d *Data) Skill(id string) (Skill, bool) { return find(d.p.Skills, d.skillIdx, id) }

// Skills are every skill, in file order.
func (d *Data) Skills() []Skill { return d.p.Skills }

// Learnable reports whether a character of class c can put points in the skill: its class teaches it, or a class above it does.
func (c Class) Learnable(s Skill) bool { return c.Fits(s.Class) && s.Class != "" }
