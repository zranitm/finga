package world

import "time"

// FieldBoss says the map's field boss is up, to the map as it appears and in the Snapshot of anyone joining while it is: its mob and its type. It stays until it's beaten.
type FieldBoss struct {
	Tick uint64
	Mob  uint64
	Type string
}

// FieldBossGone says the map's field boss is beaten, to the map as it falls and in the Snapshot of anyone joining while it's down: the mob it was, its type, and how long until it's back at its spot.
type FieldBossGone struct {
	Tick   uint64
	Mob    uint64
	Type   string
	BackIn time.Duration
}

func (FieldBoss) event()     {}
func (FieldBossGone) event() {}
