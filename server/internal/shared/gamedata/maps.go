package gamedata

import (
	"math"
	"time"

	"finga/internal/shared/nav"
	"finga/internal/shared/sim"
)

// MapKind separates safe towns, hunting maps and matches' maps.
type MapKind string

// The map kinds.
const (
	Town    MapKind = "town"
	Hunting MapKind = "hunting"
	Pvp     MapKind = "pvp" // a match's map: no portal, teleport or world chart reaches it
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

// BossSpot is where a hunting map's field boss stands, at its circle's centre.
type BossSpot struct {
	Mob    string
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
	Boss               *BossSpot    // a hunting map's; nil in a town
	Grid               *nav.Grid    // the walk grid, built at load so no tick waits on it
	Sides              [2][]Spot    // a pvp map's starting spots, side 1's then side 2's
	Walled             sim.Geometry // a pvp map's Geometry with its prep wall standing, before a round starts
}

// Spot is where a fighter starts, facing Yaw: radians from north toward east.
type Spot struct {
	At  Point
	Yaw float64
}

// Holds reports whether feet at (x, y, z) are inside the map and in no solid rising over step above them; NaN never holds.
func (m *Map) Holds(x, y, z, step float64) bool {
	g := m.Geometry
	if !(x >= 0 && x <= g.SizeX && z >= 0 && z <= g.SizeZ) || math.IsNaN(y) {
		return false
	}
	return !g.SolidOver(x, z, y+step)
}

// Maps lists every map, in file-name order.
func (d *Data) Maps() []*Map { return d.p.Maps }

// Map finds a map by ID (its file name without the extension).
func (d *Data) Map(id string) (*Map, bool) { return find(d.p.Maps, d.mapIdx, id) }

// StartTown is the town new characters start in, the one map with the property start.
func (d *Data) StartTown() *Map { return d.p.StartTown }
