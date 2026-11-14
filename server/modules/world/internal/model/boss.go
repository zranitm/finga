package model

import "time"

// FieldBoss says the map's field boss is up, to the map as it appears and in the Snapshot of anyone joining while it is: its mob, its type, and how long until it leaves.
type FieldBoss struct {
	Tick     uint64
	Mob      uint64
	Type     string
	LeavesIn time.Duration
}

// FieldBossGone says the map's field boss fell or, unbeaten, left.
type FieldBossGone struct {
	Tick   uint64
	Mob    uint64
	Beaten bool
}

func (FieldBoss) event()     {}
func (FieldBossGone) event() {}

// Announcement is news a map sends every player online, through the social module.
//
//sumtype:decl
type Announcement interface{ announcement() }

// BossAppeared says a map's field boss appeared.
type BossAppeared struct {
	Boss string // its mob type
	Map  string
}

func (BossAppeared) announcement() {}
