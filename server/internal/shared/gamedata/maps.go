package gamedata

import (
	"math"
	"time"

	"finga/internal/shared/nav"
	"finga/internal/shared/regiongen"
	"finga/internal/shared/sim"
)

// MapKind separates safe towns, hunting maps and matches' maps.
type MapKind string

// The map kinds.
const (
	Town    MapKind = "town"
	Hunting MapKind = "hunting"
	Pvp     MapKind = "pvp"    // a match's map: no portal, teleport or world chart reaches it
	Region  MapKind = "region" // generated from its plan in data/regions, of towns and hunting areas (large-regions.md section 4)
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

// Area is one of a region's towns or hunting areas (large-regions.md section 6).
type Area struct {
	ID, Name           string
	Kind               MapKind // Town or Hunting
	MinLevel, MaxLevel int     // a hunting area's mobs are of these levels; 0 in a town
	Boss               *BossSpot
}

// WaypointReach is how near a waypoint's stone a player comes to find it, in metres.
const WaypointReach = 12

// Waypoint is a region's waypoint: a stone found within WaypointReach, where its dead get up.
type Waypoint struct {
	ID, Name string
	At       Point   // the stone
	Arrive   Point   // beside it, where a teleport lands and the dead get up
	Yaw      float64 // facing there, radians from north toward east: north, the camera's way
	Main     bool    // in the region's main town, the town holding its start
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
	Town               string // the town its players respawn in; a town names itself, and so does a region, whose players get up at its start
	MinLevel, MaxLevel int    // a hunting map's mobs are of these levels; 0 for a town or a region, whose areas have their own
	Geometry           sim.Geometry
	Portals            []Portal
	Spawns             []Spawn
	PlayerSpawn        Point   // where players appear; in towns, the respawn point
	SpawnYaw           float64 // the way they face there, radians from north toward east
	NPCs               []Placement
	Boss               *BossSpot                // a hunting map's; nil in a town
	Grid               func() *nav.Grid         // the walk grid, built at the first call; the world calls every map's as it starts, so no tick waits on one
	Sides              [2][]Spot                // a pvp map's starting spots, side 1's then side 2's
	Walled             sim.Geometry             // a pvp map's Geometry with its prep wall standing, before a round starts
	Bases              [2]Circle                // a battle map's, side 1's then side 2's; zero on another map
	Points             []CapturePoint           // a battle map's capture points
	Kept               [2]sim.Geometry          // what each side of a battle walks: Geometry with the other side's base standing
	KeptGrid           func(side int) *nav.Grid // the walk grid of a side's Kept, built at the first call, as Grid; nil on a map with no bases
	Bake               string                   // a region's bake hash, the first 16 hex digits, naming the folder its client files are in; empty on a map
	Areas              []Area                   // a region's, in its plan's order; none on a map
	AreaGrid           regiongen.AreaGrid       // which of Areas holds each 4 m square of a region
	Waypoints          []Waypoint               // a region's, ids unique across every region; every town area holds one
	Under              string                   // a place below's region above, which its gates lead up to; empty on the land and on a map
}

// Circle is a round area: At and what's within Radius of it.
type Circle struct {
	At     Point
	Radius float64
}

// Holds reports whether (x, z) is within the circle.
func (c Circle) Holds(x, z float64) bool {
	return math.Hypot(x-c.At.X, z-c.At.Z) <= c.Radius
}

// CapturePoint is a battle map's point: the circle a side stands in to take it, by its id and the name players see.
type CapturePoint struct {
	ID, Name string
	Circle
}

// Spot is where a fighter starts, facing Yaw: radians from north toward east.
type Spot struct {
	At  Point
	Yaw float64
}

// AreaAt is the index in Areas of the area holding (x, z): -1 on a map, or in a region's ring.
func (m *Map) AreaAt(x, z float64) int {
	if len(m.Areas) == 0 {
		return -1
	}
	return m.AreaGrid.Holding(x, z)
}

// TownAt reports whether (x, z) is safe from harm: anywhere on a town map, and in a region's town areas.
func (m *Map) TownAt(x, z float64) bool { return m.Kind == Town || m.TownAreaAt(x, z) }

// TownAreaAt reports whether (x, z) lies in one of a region's town areas, whose line its mobs never cross.
func (m *Map) TownAreaAt(x, z float64) bool {
	a := m.AreaAt(x, z)
	return a >= 0 && m.Areas[a].Kind == Town
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

// Waypoint is the waypoint id and the region it stands in.
func (d *Data) Waypoint(id string) (*Map, Waypoint, bool) {
	at, ok := d.waypointIdx[id]
	if !ok {
		return nil, Waypoint{}, false
	}
	m := d.p.Maps[at.m]
	return m, m.Waypoints[at.w], true
}

// GetUp is where one who died at (x, z) on m gets up, the map and the way they face: on a map that revives its own dead, beside its nearest waypoint in a straight line, found or not, on a region, whose towns each hold one, or at the spawn on a town; elsewhere at its town's spawn.
func (d *Data) GetUp(m *Map, x, z float64) (*Map, Point, float64) {
	if m.Town != m.ID {
		town, _ := d.Map(m.Town) // loading checks that every map names one
		return town, town.PlayerSpawn, town.SpawnYaw
	}
	at, yaw, best := m.PlayerSpawn, m.SpawnYaw, math.Inf(1)
	for _, w := range m.Waypoints {
		if far := math.Hypot(w.At.X-x, w.At.Z-z); far < best {
			at, yaw, best = w.Arrive, w.Yaw, far
		}
	}
	return m, at, yaw
}

// StartTown is the town new characters start in, the one map with the property start.
func (d *Data) StartTown() *Map { return d.p.StartTown }
