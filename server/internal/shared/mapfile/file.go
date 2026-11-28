// Package mapfile is the 3D map format (docs/tech/3d-world.md section 4): its YAML, and the sim's geometry and walk grid built from it.
package mapfile

import "time"

// File is one map as written in data/maps/<id>.yaml. Lengths are metres, angles degrees, positions [x, z] with x east and z north.
type File struct {
	Name        string         `yaml:"name"`
	Kind        string         `yaml:"kind"` // pvp; a region's plan is read as a map of kind region
	Start       bool           `yaml:"start,omitempty"`
	Look        Look           `yaml:"look,flow"`
	Size        [2]float64     `yaml:"size,flow"` // x by z; the edges are walls
	Terrain     Terrain        `yaml:"terrain"`
	Solids      []Solid        `yaml:"solids,omitempty"`
	Props       []Prop         `yaml:"props,omitempty"`
	Portals     []Portal       `yaml:"portals"`          // a region's gates; a match's map has none
	Spawns      []Spawn        `yaml:"spawns,omitempty"` // a region's camps
	PlayerSpawn Place          `yaml:"player_spawn,flow"`
	NPCs        []NPC          `yaml:"npcs,omitempty"`
	Sides       [][]Place      `yaml:"sides,omitempty"`     // a pvp map's two sides, each its starting spots
	PrepWall    []Solid        `yaml:"prep_wall,omitempty"` // a pvp map's solids standing only before a round starts; a battle map's close its bases
	Bases       []Base         `yaml:"bases,omitempty"`     // a battle map's two, side 1's then side 2's
	Points      []CapturePoint `yaml:"points,omitempty"`    // a battle map's
}

// Base is a battle side's circle at At: its spots stand in it, it heals its own side, and the other side's bodies can't enter it.
type Base struct {
	At     [2]float64 `yaml:"at,flow"`
	Radius float64    `yaml:"radius"`
}

// CapturePoint is a battle map's point: the circle a side stands in to take it.
type CapturePoint struct {
	ID     string     `yaml:"id"`
	Name   string     `yaml:"name"`
	At     [2]float64 `yaml:"at,flow"`
	Radius float64    `yaml:"radius"`
}

// Look is the client's art keys for the ground and the sky.
type Look struct {
	Ground string `yaml:"ground"`
	Sky    string `yaml:"sky"`
}

// Terrain is the height grid's spacing and what raises it from 0: each hill and each mesa adds its height.
type Terrain struct {
	Cell  float64 `yaml:"cell"`
	Hills []Hill  `yaml:"hills,omitempty"`
	Mesas []Mesa  `yaml:"mesas,omitempty"`
}

// Hill is a smooth bump, Height at At falling to 0 at Radius.
type Hill struct {
	At     [2]float64 `yaml:"at,flow"`
	Radius float64    `yaml:"radius"`
	Height float64    `yaml:"height"`
}

// Mesa is raised flat ground over Area, [x, z, width, depth] from its south-west corner, sloping to 0 over Edge outside it.
type Mesa struct {
	Area   [4]float64 `yaml:"area,flow"`
	Height float64    `yaml:"height"`
	Edge   float64    `yaml:"edge"`
}

// Solid is an upright box or cylinder standing Height above the terrain at its centre; a top low enough is stood on.
type Solid struct {
	Box      *[4]float64 `yaml:"box,omitempty,flow"`      // [x, z, width, depth] about its centre, width across its yaw and depth along it
	Cylinder *[2]float64 `yaml:"cylinder,omitempty,flow"` // its centre, [x, z]
	Radius   float64     `yaml:"radius,omitempty"`        // a cylinder's
	Height   float64     `yaml:"height"`
	Yaw      float64     `yaml:"yaw,omitempty"` // a box's turn from facing north toward east
	Model    string      `yaml:"model"`
}

// Prop is looks only: one model At a spot, or a Scatter the client expands from its seed.
type Prop struct {
	At      *[2]float64 `yaml:"at,omitempty,flow"`
	Yaw     float64     `yaml:"yaw,omitempty"`
	Model   string      `yaml:"model,omitempty"`
	Scatter *Scatter    `yaml:"scatter,omitempty,flow"`
}

// Scatter is Count of one model strewn over Area, [x, z, width, depth] from its south-west corner.
type Scatter struct {
	Model string     `yaml:"model"`
	Area  [4]float64 `yaml:"area,flow"`
	Count int        `yaml:"count"`
	Seed  int64      `yaml:"seed"`
}

// Portal is a circle leading to a named portal on another map; players arriving through it stand at its centre.
type Portal struct {
	Name   string     `yaml:"name"`
	At     [2]float64 `yaml:"at,flow"`
	Radius float64    `yaml:"radius"`
	To     Target     `yaml:"to,flow"`
}

// Target is the map and portal a portal leads to.
type Target struct {
	Map    string `yaml:"map"`
	Portal string `yaml:"portal"`
}

// Spawn keeps up to Max of one mob inside its circle; a missing one returns after Respawn.
type Spawn struct {
	Name    string        `yaml:"name"`
	At      [2]float64    `yaml:"at,flow"`
	Radius  float64       `yaml:"radius"`
	Mob     string        `yaml:"mob"`
	Max     int           `yaml:"max"`
	Respawn time.Duration `yaml:"respawn"`
}

// Place is where something stands and the way it faces.
type Place struct {
	At  [2]float64 `yaml:"at,flow"`
	Yaw float64    `yaml:"yaw"`
}

// NPC is an NPC of npcs.yaml standing on the map.
type NPC struct {
	ID  string     `yaml:"id"`
	At  [2]float64 `yaml:"at,flow"`
	Yaw float64    `yaml:"yaw"`
}

// Boss is where a hunting area's field boss stands, wandering its circle.
type Boss struct {
	At     [2]float64 `yaml:"at,flow"`
	Radius float64    `yaml:"radius"`
	Mob    string     `yaml:"mob"`
}
