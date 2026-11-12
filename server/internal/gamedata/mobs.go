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
	Damage          DamageType // what its contact attack deals
	PhysicalDefense int
	MagicDefense    int
	Accuracy        int
	Evasion         int
	Exp             int64
	Speed           float64       // px/s
	TouchGuard      time.Duration // after its touch, hit or MISS, how long the player's next touch from any mob waits
	Width           float64
	Height          float64
	Behavior        string
	Drops           []Drop
}

// Mobs lists every mob type in file order.
func (d *Data) Mobs() []Mob { return d.p.Mobs }

// Mob finds a mob type.
func (d *Data) Mob(id string) (Mob, bool) { return find(d.p.Mobs, d.mobIdx, id) }

// FieldBossRules are what every field boss shares.
type FieldBossRules struct {
	Life      time.Duration // how long one nobody beats stays
	DropShare float64       // the share of its HP a player's damage, or their party's, must reach for a drop roll
}

// FieldBoss is the rules every field boss shares.
func (d *Data) FieldBoss() FieldBossRules { return d.p.FieldBoss }
