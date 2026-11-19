package gamedata

import "time"

// Loot is how pouches and a boss's boxes behave on the ground.
type Loot struct {
	Pouch     PouchChance   // the chance a kill leaves a pouch
	Reach     float64       // metres from a character's middle to a pouch or box to open it
	PouchLife time.Duration // a pouch still holding items then is gone
	BoxLife   time.Duration // a boss's box still holding items then is gone
}

// PouchChance is the chance a kill leaves a pouch: a normal mob's, and an elite's.
type PouchChance struct {
	Normal, Elite float64
}

// Loot is how pouches and a boss's boxes behave on the ground.
func (d *Data) Loot() Loot { return d.p.Loot }
