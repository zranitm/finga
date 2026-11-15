// Package mapfile is the 3D map format (docs/tech/3d-world.md section 4): its YAML, and the sim's geometry and walk grid built from it.
package mapfile

import "time"

// File is one map as written in data/maps/<id>.yaml. Lengths are metres, angles degrees, positions [x, z] with x east and z north.
type File struct {
	Name        string     `yaml:"name"`
	Kind        string     `yaml:"kind"` // town or hunting
	Start       bool       `yaml:"start,omitempty"`
	Town        string     `yaml:"town,omitempty"`        // a hunting map's respawn town
	Levels      [2]int     `yaml:"levels,omitempty,flow"` // a hunting map's mob levels, lowest and highest
	Look        Look       `yaml:"look,flow"`
	Size        [2]float64 `yaml:"size,flow"` // x by z; the edges are walls
	Terrain     Terrain    `yaml:"terrain"`
	Solids      []Solid    `yaml:"solids,omitempty"`
	Props       []Prop     `yaml:"props,omitempty"`
	Portals     []Portal   `yaml:"portals"`
	Spawns      []Spawn    `yaml:"spawns,omitempty"`
	PlayerSpawn Place      `yaml:"player_spawn,flow"`
	NPCs        []NPC      `yaml:"npcs,omitempty"`
	Boss        *Boss      `yaml:"boss,omitempty,flow"`
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

// Boss is where a hunting map's field boss appears, and how many kills on the map bring it.
type Boss struct {
	At     [2]float64 `yaml:"at,flow"`
	Radius float64    `yaml:"radius"`
	Mob    string     `yaml:"mob"`
	Kills  int        `yaml:"kills"`
}
