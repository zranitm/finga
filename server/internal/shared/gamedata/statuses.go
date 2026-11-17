package gamedata

import "time"

// StatusGroup is a status's kind: hard or soft control, damage over time, weakened, or a mob's aggro.
type StatusGroup string

// The status groups.
const (
	Hard   StatusGroup = "hard"
	Soft   StatusGroup = "soft"
	Dot    StatusGroup = "dot"
	Weaken StatusGroup = "weaken"
	Aggro  StatusGroup = "aggro" // mobs only
)

// The statuses the world gives more than their data: the down pose, and a mob's aggro.
const (
	Knockdown = "knockdown"
	Taunt     = "taunt"      // its provoker is the inflicter
	LoseSight = "lose_sight" // drops the inflicter, and can't see them
)

// Blocks are what a status stops its body doing, a set.
type Blocks uint8

// The blocks.
const (
	BlocksMove   Blocks = 1 << iota // no walking, chasing or moves
	BlocksAct                       // no attack or skill, and one under way ends
	BlocksSkills                    // no skill; basic attacks go
)

// Has is whether b holds every block of o.
func (b Blocks) Has(o Blocks) bool { return b&o == o }

// Status is an abnormal status, in data/statuses.yaml; a factor it doesn't name is 1.
type Status struct {
	ID              string
	Group           StatusGroup
	Blocks          Blocks
	MoveSpeed       float64
	Accuracy        float64
	Attack          float64
	PhysicalDefense float64
	MagicDefense    float64
	Shatter         float64       // the next hit that lands ends it, times this; 0 for none
	Wakes           bool          // any damage ends it
	Every           time.Duration // damage over time: how often it ticks
	Moving          float64       // damage over time: a tick's factor when the body moved since the last
}

// StatusRules are what every status shares.
type StatusRules struct {
	SuperArmor float64 // the share of its time a status other than a hard one lasts on super armor, which hard ones don't take
}

// Inflict is a status a hit puts on what it lands on, for Time; damage over time deals Power × the inflicter's attack each tick. A knockdown names no time: it lasts the down pose.
type Inflict struct {
	Status string
	Time   time.Duration
	Power  float64
}

// SkillInflict is a skill's Inflict, its power growing with the points invested.
type SkillInflict struct {
	Status string
	Time   time.Duration
	Power  Scaled
}

// At is the inflict with points invested.
func (s SkillInflict) At(points int) Inflict {
	return Inflict{Status: s.Status, Time: s.Time, Power: s.Power.At(points)}
}

// Status is the status with id.
func (d *Data) Status(id string) (Status, bool) { return find(d.p.Statuses, d.statusIdx, id) }

// StatusRules are the rules every status shares.
func (d *Data) StatusRules() StatusRules { return d.p.StatusRules }
