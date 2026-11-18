package gamedata

import (
	"slices"
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

// SkillEffect is what a skill does: an active one damages, heals, buffs, puts a status or a push on mobs, or only moves its user; a passive one adds a bonus.
type SkillEffect string

// The skill effects.
const (
	DamageEffect SkillEffect = "damage"
	HealEffect   SkillEffect = "heal"
	BuffEffect   SkillEffect = "buff"
	StatusEffect SkillEffect = "status" // its status or push on the mobs in its shape, with no damage
	MoveEffect   SkillEffect = "move"   // its move alone, with no shape
	BlockEffect  SkillEffect = "block"  // its block, and its move if it has one: no shape, power or damage
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
	At                                  ShapeAt // an area's middle
	Reach                               float64 // with At: how far off the middle may be
}

// ShapeAt is where an area centres: on the user, or on their lock.
type ShapeAt string

// The centres: the user's feet, or the locked mob within Reach, else Reach along the aim.
const (
	AtUser ShapeAt = ""
	AtLock ShapeAt = "lock"
)

// MoveKind is how a skill moves its user.
type MoveKind string

// The move kinds.
const (
	DashMove  MoveKind = "dash"  // along the ground at Distance ÷ Time
	LeapMove  MoveKind = "leap"  // as a dash, with a hop that lands as Time runs out
	BlinkMove MoveKind = "blink" // at once, to the farthest standable point along the way
)

// Toward is the way a move goes.
type Toward string

// The ways.
const (
	TowardLock    Toward = "lock"    // at the locked mob: a dash or leap to its near contact ring, a blink to its far one, behind it
	TowardBack    Toward = "back"    // away from the lock, or back from the heading with none
	TowardHeading Toward = "heading" // along the keys' heading, or back from it while standing
)

// Move is a skill's move of its user.
type Move struct {
	Kind     MoveKind
	Distance float64       // metres, at most
	Time     time.Duration // dash and leap
	Toward   Toward
	Stop     bool // dash and leap: the move ends at the first mob body it meets
	Through  bool // dash and leap: the move passes mobs' bodies and ends clear of them, as a blink does
}

// Hits repeat a skill's hit Count times, Every apart, from its hit point.
type Hits struct {
	Count int
	Every time.Duration
}

// Shield is a buff's shield: Power × its user's magic attack, taken before HP.
type Shield struct {
	Power Scaled
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
	Icon        string          // a game-icons.net path, author/name, that the client draws
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
	Move        Move            // active; move's own
	Dodge       time.Duration   // active: from the action's start, mobs' attacks pass through the user
	Block       time.Duration   // block: from the action's start, the user stops blockable mob attacks from the front
	Status      SkillInflict    // damage and status: on what it lands on
	Push        float64         // damage and status: metres what it lands on is pushed away from the user
	Hits        Hits            // damage: the hit repeats; Count 0 hits once
	ReachParty  bool            // buff: the caster and party members within the area's radius
	OnHit       SkillInflict    // buff: on what each of the user's hits lands on while it lasts
	Shield      Shield          // buff
}

// Skill is a skill and its evolutions, in data/skills.yaml.
type Skill struct {
	ID     string
	Kind   SkillKind
	Effect SkillEffect
	Class  kernel.Class // the class that teaches it
	Level  int          // the character level from which it takes its first point
	Stages []Stage      // the first at 0 points, the totals rising
	// BreaksControl is a break-free skill's: usable only while its user holds a hard status, which it ends first, through cant-act and silence.
	BreaksControl bool
	// Granted is a skill its class has without points: it counts as one and takes no more.
	Granted bool
	// Traits are an active skill's default and extra, or none: the default is the skill as its stages write it.
	Traits []Trait
}

// Trait is one of a skill's two ways to behave; Stage is the extra's, its changes on top of the skill's last stage, which keeps that stage's name and description.
type Trait struct {
	ID, Name, Description string
	Stage                 Stage
}

// PointsIn is the points a character with invested in s has in it: a granted skill's are one.
func (s Skill) PointsIn(invested int) int {
	if s.Granted {
		return 1
	}
	return invested
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

// TraitOpen reports whether a skill with points has reached its last stage, where its extra trait opens; one with a single stage opens it at its first point.
func (s Skill) TraitOpen(points int) bool { return points >= max(s.Stages[len(s.Stages)-1].At, 1) }

// Trait is the index of s's trait with id, 0 its default and 1 its extra; false for one it doesn't have.
func (s Skill) Trait(id string) (int, bool) {
	i := slices.IndexFunc(s.Traits, func(t Trait) bool { return t.ID == id })
	return i, i >= 0
}

// Spec is what a use of s with points and trait resolves to: the extra's stage once it's open, else the stage the points reach; an unknown or empty trait is the default.
func (s Skill) Spec(points int, trait string) Stage {
	if i, ok := s.Trait(trait); ok && i == 1 && s.TraitOpen(points) {
		return s.Traits[1].Stage
	}
	return s.Stages[s.StageAt(points)]
}

// Skill is the skill with id.
func (d *Data) Skill(id string) (Skill, bool) { return find(d.p.Skills, d.skillIdx, id) }

// Skills are every skill, in file order.
func (d *Data) Skills() []Skill { return d.p.Skills }

// Learnable reports whether a character of class c has the skill to use: its class teaches it, or a class above it does; a granted skill is its own class's alone, since a promotion grants its own.
func (c Class) Learnable(s Skill) bool {
	if s.Granted {
		return s.Class != "" && c.ID == s.Class
	}
	return c.Fits(s.Class) && s.Class != ""
}
