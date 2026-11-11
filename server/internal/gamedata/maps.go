package gamedata

import (
	"slices"
	"time"

	"finga/internal/sim"
)

// MapKind separates safe towns from hunting maps.
type MapKind string

// The map kinds.
const (
	Town    MapKind = "town"
	Hunting MapKind = "hunting"
)

// Point is a position in map pixels: a character's feet centre.
type Point struct {
	X, Y float64
}

// Portal leads to a named portal on another map.
type Portal struct {
	Name         string
	Area         sim.Rect
	TargetMap    string
	TargetPortal string
}

// Arrival is where a character arriving through p stands: its rectangle's bottom centre.
func (p Portal) Arrival() Point {
	return Point{X: p.Area.X + p.Area.W/2, Y: p.Area.Y + p.Area.H}
}

// Spawn keeps up to Max mobs of one type inside Area; a missing one returns after Respawn.
type Spawn struct {
	Mob     string
	Max     int
	Respawn time.Duration
	Area    sim.Rect
}

// Map is one Tiled map, converted.
type Map struct {
	ID          string
	Name        string // what players see, as the banner on entering
	Kind        MapKind
	Start       bool   // new characters start here; exactly one town has it
	Town        string // the town its players respawn in; a town names itself
	MinLevel    int    // a hunting map's mobs are of these levels; 0 for a town
	MaxLevel    int
	Width       float64 // pixels
	Height      float64
	Geometry    sim.Geometry
	Portals     []Portal
	Spawns      []Spawn
	PlayerSpawn Point // where players appear; in towns, the respawn point
	NPCs        []Placement
}

// Holds reports whether a character of body's size can stand at (x, y): inside the map and clear of every solid. NaN fails every comparison, so it never holds.
func (m *Map) Holds(x, y float64, body sim.Params) bool {
	if !(x >= 0 && x <= m.Width && y >= 0 && y <= m.Height) {
		return false
	}
	box := sim.Rect{X: x - body.Width/2, Y: y - body.Height, W: body.Width, H: body.Height}
	return !slices.ContainsFunc(m.Geometry.Solids, box.Overlaps)
}

// Placement is an NPC standing on a map, from its npcs layer.
type Placement struct {
	NPC string
	At  Point
}

// Maps lists every map, in file-name order.
func (d *Data) Maps() []*Map { return d.p.Maps }

// Map finds a map by ID (its file name without the extension).
func (d *Data) Map(id string) (*Map, bool) { return find(d.p.Maps, d.mapIdx, id) }

// StartTown is the town new characters start in, the one map with the property start.
func (d *Data) StartTown() *Map { return d.p.StartTown }
