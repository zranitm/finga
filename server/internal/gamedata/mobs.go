package gamedata

import "time"

// Drop is one entry of a mob's drop table; each rolls independently.
type Drop struct {
	Item   string
	Chance float64 // (0, 1]
	Min    int
	Max    int
}

// Mob is a mob type.
type Mob struct {
	ID              string
	Name            string
	Level           int
	Elite           bool // a map's bigger, rarer mob, with more HP, EXP and drops than its normal one
	Boss            bool // a hunting map's field boss, raised by kills on the map rather than spawned
	HP              int
	Attack          int
	Damage          DamageType // what its attacks deal
	PhysicalDefense int
	MagicDefense    int
	Accuracy        int
	Evasion         int
	Exp             int64
	Speed           float64 // px/s
	Width           float64
	Height          float64
	Behavior        string
	SuperArmor      bool        // never flinches, is pushed or knocked down, and its attacks go on whatever hits it
	Attacks         []MobAttack // a boss alternates its two
	Drops           []Drop
}

// MobAttackKind is how a mob's attack moves and what it reaches, from the mob's box and facing.
type MobAttackKind string

// The attack kinds.
const (
	Front  MobAttackKind = "front"  // Reach px before its front edge, at its height
	Leap   MobAttackKind = "leap"   // up to Distance toward its target over Travel, then its box grown by Spread where it lands
	Rush   MobAttackKind = "rush"   // up to Distance forward over Travel, hitting the first player its box meets
	Shot   MobAttackKind = "shot"   // a Size square from its front at mid-height, flying Speed px/s up to Range, hitting the first player it meets
	Around MobAttackKind = "around" // its box grown by Spread on every side
)

// MobAttack is one of a mob's attacks: Windup, the act (Travel long for a leap or a rush), then Recover; Cooldown counts from its start.
type MobAttack struct {
	Name     string
	Kind     MobAttackKind
	Windup   time.Duration
	Travel   time.Duration
	Recover  time.Duration
	Cooldown time.Duration
	Power    float64 // times the mob's attack
	Reach    float64 // px
	Distance float64 // px
	Spread   float64 // px
	Range    float64 // px
	Speed    float64 // px/s
	Size     float64 // px
}

// Busy is how long the attack keeps its mob from another: its windup, act and recover.
func (a MobAttack) Busy() time.Duration { return a.Windup + a.Travel + a.Recover }

// Mobs lists every mob type in file order.
func (d *Data) Mobs() []Mob { return d.p.Mobs }

// Mob finds a mob type.
func (d *Data) Mob(id string) (Mob, bool) { return find(d.p.Mobs, d.mobIdx, id) }

// MobRules are what every mob's attacks share.
type MobRules struct {
	HitGuard time.Duration // after a mob's hit or MISS, no mob's attack hits the player for this long; a hit blinks for it
	Provoke  time.Duration // how long a mob a player hits follows them
	Sight    float64       // how far either way a mob sees a player whose feet are level with its own or a little above
	React    MobReact
	Patrol   MobPatrol

	EngageMin, EngageMax time.Duration // each time someone comes into a mob's reach, after nobody was, it waits a time rolled between them before a windup
}

// MobPatrol is each mob's own pace, so mobs that meet drift apart again.
type MobPatrol struct {
	SpeedJitter        float64       // a mob walks, chases and rushes at 1 ± up to this of its speed, rolled at its spawn
	PauseMin, PauseMax time.Duration // each pause at an edge is rolled between them
}

// MobReact is how a mob without super armor takes a player's blow that lands.
type MobReact struct {
	Flinch     time.Duration // it stops this long, its windup cancelled
	Push       float64       // px it's pushed away from the attacker
	DownHits   int           // blows each within DownWindow of the last knock it down
	DownWindow time.Duration
	Down       time.Duration // how long it lies, still taking damage
	ArmorAfter time.Duration // its super armor once it gets up
}

// MobRules is the rules every mob's attacks share.
func (d *Data) MobRules() MobRules { return d.p.MobRules }

// FieldBossRules are what every field boss shares.
type FieldBossRules struct {
	Life      time.Duration // how long one nobody beats stays
	DropShare float64       // the share of its HP a player's damage, or their party's, must reach for a drop roll
}

// FieldBoss is the rules every field boss shares.
func (d *Data) FieldBoss() FieldBossRules { return d.p.FieldBoss }
