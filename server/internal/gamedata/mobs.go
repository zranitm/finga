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
	HP              int
	Attack          int
	Damage          DamageType // what its contact attack deals
	PhysicalDefense int
	MagicDefense    int
	Accuracy        int
	Evasion         int
	Exp             int64
	Speed           float64 // px/s
	Width           float64
	Height          float64
	Behavior        string
	Drops           []Drop
}

// Mobs lists every mob type in file order.
func (d *Data) Mobs() []Mob { return d.p.Mobs }

// Mob finds a mob type.
func (d *Data) Mob(id string) (Mob, bool) { return find(d.p.Mobs, d.mobIdx, id) }

// TouchGuard is how long after a mob's touch, hit or MISS, the next touch waits.
func (d *Data) TouchGuard() time.Duration { return d.p.TouchGuard }
