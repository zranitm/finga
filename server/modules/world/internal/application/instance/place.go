package instance

import (
	"math"

	"finga/internal/gamedata"
	"finga/internal/sim"
	"finga/modules/world/internal/model"
)

const (
	fallDepth    = 10.0 // metres below the map's lowest ground past which a falling player is taken back to its spawn
	portalHeight = 1.0  // metres from a portal's ground within which feet in its circle stand in it
)

// standOn is a body at rest on m's ground under (x, z), facing yaw: the terrain there, or a solid's top no more than a step above its
// feet at y or the terrain, as a join, a respawn, a rescue or a portal places one.
func standOn(m *gamedata.Map, p sim.Params, x, y, z float64, yaw float32) sim.State {
	hx, hz := model.HeadingOf(yaw)
	return sim.State{X: x, Y: groundUnder(m, p, x, z, y), Z: z, OnGround: true, HX: hx, HZ: hz}
}

// groundUnder is the ground at (x, z) for feet at y: the terrain, or a solid's top no more than a step above them or the terrain.
func groundUnder(m *gamedata.Map, p sim.Params, x, z, y float64) float64 {
	g := m.Geometry
	return sim.Ground(x, z, max(y, g.Terrain.Height(x, z))+p.Step, g)
}

// standing is standOn on this map.
func (i *Instance) standing(x, y, z float64, yaw float32) sim.State {
	return standOn(i.m, i.params, x, y, z, yaw)
}

// fellOut reports whether s has fallen fallDepth below the map's lowest ground, or lost its position to NaN.
func (i *Instance) fellOut(s sim.State) bool {
	return s.Y < i.lowest-fallDepth || math.IsNaN(s.X) || math.IsNaN(s.Y) || math.IsNaN(s.Z)
}

// bodyOf is p's cylinder, as shapes and mobs reach it.
func (i *Instance) bodyOf(p *player) model.Body {
	return model.Body{X: p.state.X, Y: p.state.Y, Z: p.state.Z, Radius: i.params.Radius, Height: i.params.Height}
}

// facing is the yaw s faces: its heading's.
func facing(s sim.State) float32 { return model.YawOf(s.HX, s.HZ) }

// awayFrom is the yaw from a portal at (x, z) into m, toward its middle: the way a player arriving through it faces.
func awayFrom(m *gamedata.Map, x, z float64) float32 {
	return model.YawOf(m.Geometry.SizeX/2-x, m.Geometry.SizeZ/2-z)
}

// land is where a drop thrown to (x, z) from feet at y comes to rest: inside the map, on the ground under it.
func (i *Instance) land(x, z, y float64) (float64, float64, float64) {
	x, z = min(max(x, 0), i.m.Geometry.SizeX), min(max(z, 0), i.m.Geometry.SizeZ)
	return x, groundUnder(i.m, i.params, x, z, y), z
}
