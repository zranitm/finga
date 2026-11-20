package world

import (
	"maps"
	"time"
)

// Waypoints are the waypoints a character has found, by id across every region, with when.
type Waypoints map[string]time.Time

// Clone copies the waypoints found.
func (w Waypoints) Clone() Waypoints { return maps.Clone(w) }

// WaypointFound tells the player they came within reach of a waypoint's stone they hadn't found.
type WaypointFound struct {
	Tick     uint64
	Waypoint string
}

func (WaypointFound) event() {}
