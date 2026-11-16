package gamedata

import "time"

// Loot is how drops behave on the ground.
type Loot struct {
	CollectDelay   time.Duration // from the kill until a drop flies to its owner
	DropLife       time.Duration // a drop still on the ground then is lost
	RoomCheckEvery time.Duration // how often an owner's drops waiting for room are tried again
}

// Loot is how drops behave on the ground.
func (d *Data) Loot() Loot { return d.p.Loot }
