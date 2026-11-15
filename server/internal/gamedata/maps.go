package gamedata

import (
	"math"
	"time"

	"finga/internal/nav"
	"finga/internal/sim"
)

// MapKind separates safe towns from hunting maps.
type MapKind string

// The map kinds.
const (
	Town    MapKind = "town"
	Hunting MapKind = "hunting"
)

// Point is a spot on a map: x east and z north, in metres. What stands there stands on the ground under it.
type Point struct {
	X, Z float64
}

// Portal is a circle leading to a named portal on another map; a character arriving through it stands at its centre.
type Portal struct {
	Name         string
	At           Point
	Radius       float64
	TargetMap    string
	TargetPortal string
}

// Spawn keeps up to Max mobs of one type inside its circle; a missing one returns after Respawn.
type Spawn struct {
	Mob     string
	Max     int
	Respawn time.Duration
	At      Point
	Radius  float64
}

// BossSpot is where a hunting map's field boss appears, at its circle's centre, and how many kills on the map bring it.
type BossSpot struct {
	Mob    string
	Kills  int
	At     Point
	Radius float64 // it wanders the circle
}

// Placement is an NPC standing on a map, facing Yaw: radians from north toward east.
type Placement struct {
	NPC string
	At  Point
	Yaw float64
}

// Map is one map, built for the sim (docs/tech/3d-world.md section 4).
type Map struct {
	ID                 string
	Name               string // what players see, as the banner on entering
	Kind               MapKind
	Start              bool   // new characters start here; exactly one town has it
	Town               string // the town its players respawn in; a town names itself
	MinLevel, MaxLevel int    // a hunting map's mobs are of these levels; 0 for a town
	Geometry           sim.Geometry
	Portals            []Portal
	Spawns             []Spawn
	PlayerSpawn        Point   // where players appear; in towns, the respawn point
	SpawnYaw           float64 // the way they face there, radians from north toward east
	NPCs               []Placement
	Boss               *BossSpot // a hunting map's; nil in a town
	Grid               func() *nav.Grid
}

// Holds reports whether a body can stand with its feet at (x, y, z): inside the map, and not within a solid rising more than step above
// its feet. NaN never holds.
func (m *Map) Holds(x, y, z, step float64) bool {
	g := m.Geometry
	if !(x >= 0 && x <= g.SizeX && z >= 0 && z <= g.SizeZ) || math.IsNaN(y) {
		return false
	}
	for _, b := range g.Boxes {
		if b.Top > y+step && b.Holds(x, z) {
			return false
		}
	}
	for _, c := range g.Cylinders {
		if c.Top > y+step && c.Holds(x, z) {
			return false
		}
	}
	return true
}

// Maps lists every map, in file-name order.
func (d *Data) Maps() []*Map { return d.p.Maps }

// Map finds a map by ID (its file name without the extension).
func (d *Data) Map(id string) (*Map, bool) { return find(d.p.Maps, d.mapIdx, id) }

// StartTown is the town new characters start in, the one map with the property start.
func (d *Data) StartTown() *Map { return d.p.StartTown }
