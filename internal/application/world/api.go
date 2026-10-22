// Package world runs the map instances: one actor per map that ticks at
// 20 Hz, accepts players, simulates their movement from inputs (TD-18),
// and sends each player what changed. It has no repository; it reaches the
// database only through other modules' services, from worker goroutines,
// never inside a tick. Its result types are its own; the gateway turns them
// into protocol messages.
package world

import (
	"context"
	"errors"
	"time"

	"finga/internal/application/character"
	"finga/internal/application/world/sim"
	"finga/internal/model"
)

// The module's errors.
var (
	ErrBusy    = errors.New("world: map instance inbox is full")
	ErrNoMap   = errors.New("world: no such map")
	ErrStopped = errors.New("world: instance is not running")
)

// Player is what an instance needs to know about a character when it
// joins; movement state is the instance's own from then on.
type Player struct {
	ID    model.CharacterID
	Name  string
	Class model.Class
	Level int
	HP    int
	MaxHP int
	MP    int
	MaxMP int
	Exp   int64
	X, Y  float64 // where to appear: feet centre
}

// PlayerView is a player as everyone on the map sees it.
type PlayerView struct {
	ID    model.CharacterID
	Name  string
	Class model.Class
	Level int
	HP    int
	MaxHP int
	State sim.State
}

// Event is something an instance tells a player: Snapshot, Delta or
// Teleport.
type Event interface{ event() }

// Snapshot is the whole map, sent to a player on join.
type Snapshot struct {
	Tick    uint64
	MapID   string
	Players []PlayerView
}

// Delta is what changed in one tick, for one player: AckSeq is the latest
// input sequence applied for that player, Players the ones that moved or
// joined, Removed the ones that left.
type Delta struct {
	Tick    uint64
	AckSeq  uint32
	Players []PlayerView
	Removed []model.CharacterID
}

// Teleport moves the player somewhere else: a respawn (step 11) or a
// portal (step 14).
type Teleport struct {
	Tick  uint64
	MapID string
	X, Y  float64
}

func (Snapshot) event() {}
func (Delta) event()    {}
func (Teleport) event() {}

// Sink is where an instance sends a player's events. Send never blocks: the
// gateway enqueues on the connection and returns false when the queue is
// full, and the instance then drops that player, never the tick.
type Sink interface {
	Send(Event) bool
}

// Saver persists checkpoints; the character service satisfies it. It is
// called from the persistence worker, never from a tick.
type Saver interface {
	SaveCheckpoint(ctx context.Context, cp character.Checkpoint) error
}

// TickObserver sees each tick's duration against its budget;
// internal/telemetry implements it.
type TickObserver interface {
	ObserveTick(mapID string, took, budget time.Duration)
}
