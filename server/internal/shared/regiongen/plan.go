// Package regiongen builds a large region from its plan (docs/tech/large-regions.md section 3): ground, features, colours and scatter, the same bits on every CPU.
package regiongen

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
	"time"

	"gopkg.in/yaml.v3"
)

// Plan is one region as written in data/regions/<id>.yaml. Lengths are metres, positions [x, z] with x east and z north, polygons their corners in order.
type Plan struct {
	Name      string           `yaml:"name"`
	Size      [2]float64       `yaml:"size,flow"`
	Cell      float64          `yaml:"cell"` // terrain samples this far apart
	Seed      uint32           `yaml:"seed"`
	Look      Look             `yaml:"look,flow"`
	Ground    Ground           `yaml:"ground"`
	Edge      Edge             `yaml:"edge,flow"`
	Colour    Colour           `yaml:"colour,flow"`
	Features  []Feature        `yaml:"features,omitempty"`
	Areas     []Area           `yaml:"areas"`
	Camps     []Camp           `yaml:"camps,omitempty"`
	Waypoints []Waypoint       `yaml:"waypoints,omitempty"`
	NPCs      []NPC            `yaml:"npcs,omitempty"`
	Pieces    []Piece          `yaml:"pieces,omitempty"`
	Gates     []Gate           `yaml:"gates,omitempty"`
	Start     Place            `yaml:"start,flow"`
	Biomes    map[string]Biome `yaml:"biomes"`
}

// Look is the client's art key for the sky.
type Look struct {
	Sky string `yaml:"sky"`
}

// Ground is the noise the ground rolls with.
type Ground struct {
	Noise Noise `yaml:"noise,flow"`
}

// Noise is Octaves of OpenSimplex noise, the first Scale metres across, each next half the last's size and weight, the sum within about ±Amplitude.
type Noise struct {
	Scale     float64 `yaml:"scale"`
	Amplitude float64 `yaml:"amplitude"`
	Octaves   int     `yaml:"octaves"`
}

// Edge is the ring of mountains: ground rising over Width from the region's sides to between Height's two, which noise varies along it; its ground takes Biome's colours.
type Edge struct {
	Width  float64    `yaml:"width"`
	Height [2]float64 `yaml:"height,flow"`
	Biome  string     `yaml:"biome"`
}

// Colour is where a biome's colours change: Slope, a rise per metre, from flat to slope; High, a height, to high.
type Colour struct {
	Slope float64 `yaml:"slope"`
	High  float64 `yaml:"high"`
}

// Feature is one of a ridge, a plateau, a river, a lake or a road, applied in the plan's order.
type Feature struct {
	Ridge   *Ridge   `yaml:"ridge,omitempty,flow"`
	Plateau *Plateau `yaml:"plateau,omitempty,flow"`
	River   *River   `yaml:"river,omitempty,flow"`
	Lake    *Lake    `yaml:"lake,omitempty,flow"`
	Road    *Road    `yaml:"road,omitempty,flow"`
}

// Ridge raises Height along Path, falling smoothly to nothing Width/2 from it.
type Ridge struct {
	Path   [][2]float64 `yaml:"path,flow"`
	Width  float64      `yaml:"width"`
	Height float64      `yaml:"height"`
}

// Plateau raises Height over Area, its Edge a cliff or a walkable ramp.
type Plateau struct {
	Area   [][2]float64 `yaml:"area,flow"`
	Height float64      `yaml:"height"`
	Edge   string       `yaml:"edge"`
}

// The plateau edges.
const (
	Cliff = "cliff"
	Ramp  = "ramp"
)

// River carves a bed Depth below its water along Path, which runs downstream, Width across between steep banks, its water falling with the ground; at each of Fords it's shallow enough to wade.
type River struct {
	Path  [][2]float64 `yaml:"path,flow"`
	Width float64      `yaml:"width"`
	Depth float64      `yaml:"depth"`
	Fords [][2]float64 `yaml:"fords,omitempty,flow"`
}

// Lake carves Area to Depth below its water, between steep banks.
type Lake struct {
	Area  [][2]float64 `yaml:"area,flow"`
	Depth float64      `yaml:"depth"`
}

// Road is a way Width across along Path, smoothed, flattened to the ground's mean along it, and coloured earth.
type Road struct {
	Path  [][2]float64 `yaml:"path,flow"`
	Width float64      `yaml:"width"`
}

// Area is a named part of the region: a hunting ground with its levels and boss, or a town, flattened toward its mean height.
type Area struct {
	ID     string       `yaml:"id" json:"id"`
	Name   string       `yaml:"name" json:"name"`
	Area   [][2]float64 `yaml:"area,flow" json:"area"`
	Levels [2]int       `yaml:"levels,omitempty,flow" json:"levels"`
	Biome  string       `yaml:"biome" json:"biome"`
	Kind   string       `yaml:"kind" json:"kind"`
	Boss   *Boss        `yaml:"boss,omitempty,flow" json:"boss,omitempty"`
}

// The area kinds.
const (
	Hunting = "hunting"
	Town    = "town"
)

// Boss is an area's boss, summoned at At after Kills in the area.
type Boss struct {
	At     [2]float64 `yaml:"at,flow" json:"at"`
	Radius float64    `yaml:"radius" json:"radius"`
	Mob    string     `yaml:"mob" json:"mob"`
	Kills  int        `yaml:"kills" json:"kills"`
}

// Camp is a spawn: up to Max of Mob within Radius of At.
type Camp struct {
	Area    string        `yaml:"area" json:"area"`
	Name    string        `yaml:"name" json:"name"`
	At      [2]float64    `yaml:"at,flow" json:"at"`
	Radius  float64       `yaml:"radius" json:"radius"`
	Mob     string        `yaml:"mob" json:"mob"`
	Max     int           `yaml:"max" json:"max"`
	Respawn time.Duration `yaml:"respawn" json:"-"`
}

// Waypoint is a spot a player finds and teleports to.
type Waypoint struct {
	ID   string     `yaml:"id" json:"id"`
	Name string     `yaml:"name" json:"name"`
	At   [2]float64 `yaml:"at,flow" json:"at"`
}

// NPC stands At, facing Yaw degrees from north toward east.
type NPC struct {
	ID  string     `yaml:"id" json:"id"`
	At  [2]float64 `yaml:"at,flow" json:"at"`
	Yaw float64    `yaml:"yaw" json:"yaw"`
}

// Piece is a set piece placed by hand: a kit model At a spot, or a House.
type Piece struct {
	Model string      `yaml:"model,omitempty"`
	At    *[2]float64 `yaml:"at,omitempty,flow"`
	Yaw   float64     `yaml:"yaw,omitempty"`
	House *House      `yaml:"house,omitempty,flow"`
}

// House is a box solid over Area, [x, z, width, depth] from its south-west corner, Height tall, drawn as Model at its centre.
type House struct {
	Area   [4]float64 `yaml:"area,flow"`
	Height float64    `yaml:"height"`
	Model  string     `yaml:"model"`
}

// Gate is a circle leading to a gate of another region.
type Gate struct {
	Name   string     `yaml:"name" json:"name"`
	At     [2]float64 `yaml:"at,flow" json:"at"`
	Radius float64    `yaml:"radius" json:"radius"`
	To     GateTo     `yaml:"to,flow" json:"to"`
}

// GateTo names the gate a gate leads to.
type GateTo struct {
	Region string `yaml:"region" json:"region"`
	Gate   string `yaml:"gate" json:"gate"`
}

// Place is a spot and a facing.
type Place struct {
	At  [2]float64 `yaml:"at,flow" json:"at"`
	Yaw float64    `yaml:"yaw" json:"yaw"`
}

// Biome is how an area's ground is coloured and what's scattered over it.
type Biome struct {
	Colours Colours   `yaml:"colours,flow"`
	Scatter []Scatter `yaml:"scatter,omitempty"`
}

// Colours are a biome's kit ground colours: for flat ground, slopes, ground too steep to walk, and ground above the plan's high mark.
type Colours struct {
	Flat  string `yaml:"flat"`
	Slope string `yaml:"slope"`
	Steep string `yaml:"steep"`
	High  string `yaml:"high"`
}

// Scatter strews a kit model over its biome, Spacing apart at least, on ground no steeper than Slope (a rise per metre; 0 is any walkable), kept Away from what it mustn't crowd.
type Scatter struct {
	Model   string  `yaml:"model"`
	Spacing float64 `yaml:"spacing"`
	Slope   float64 `yaml:"slope,omitempty"`
	Away    Away    `yaml:"away,omitempty,flow"`
}

// Away is how far past each thing's edge a scatter keeps; it never stands on any of them.
type Away struct {
	Road     float64 `yaml:"road,omitempty"`
	Water    float64 `yaml:"water,omitempty"`
	Camp     float64 `yaml:"camp,omitempty"`
	Town     float64 `yaml:"town,omitempty"`
	Waypoint float64 `yaml:"waypoint,omitempty"` // waypoints, NPCs, gates, set pieces and the start
}

// Decode reads a plan strictly: an unknown key is an error.
func Decode(raw []byte) (Plan, error) {
	var p Plan
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err != nil {
		return Plan{}, err
	}
	return p, nil
}

// The limits a plan keeps, so a bad one is refused before it can run out of memory or reach a float-to-int conversion CPUs disagree on.
const (
	maxSide    = 8192 // a region's side
	maxReach   = 512  // a width, a radius, a spacing or an away distance
	maxHeight  = 1000 // a height, a depth or a slope
	minSpacing = 1    // scatter's
	maxAreas   = math.MaxInt16
)

// cells are the terrain spacings a plan may use: powers of two, which divide the overview's 8 m and keep every sample's position a float32's.
var cells = []float64{0.25, 0.5, 1, 2, 4, 8}

// check refuses a plan the generator can't build with kit k, naming the limit it breaks; the loader checks how its parts fit together.
func (p Plan) check(k Kit) error {
	n := p.Ground.Noise
	switch {
	case !within(p.Size[0], tileM, maxSide) || !within(p.Size[1], tileM, maxSide) || !whole(p.Size[0]/tileM) || !whole(p.Size[1]/tileM):
		return fmt.Errorf("size %v: each side whole %d m map tiles, so whole %d m chunks, up to %d m", p.Size, tileM, chunkM, maxSide)
	case !slices.Contains(cells, p.Cell):
		return fmt.Errorf("cell %g: one of %v m, a power of two", p.Cell, cells)
	case (int(p.Size[0]/p.Cell)+1)*(int(p.Size[1]/p.Cell)+1) > 1<<24:
		return fmt.Errorf("size %v at cell %g: at most 2²⁴ terrain samples", p.Size, p.Cell)
	case p.Look.Sky == "":
		return errors.New("look needs a sky")
	case n.Octaves < 1 || n.Octaves > 8:
		return fmt.Errorf("ground noise: %d octaves; 1 to 8", n.Octaves)
	case !within(n.Scale, float64(p.Cell*float64(int(1)<<(n.Octaves-1))), maxSide):
		return fmt.Errorf("ground noise scale %g: its finest octave, half as wide for each after the first, at least a cell across, and the scale at most %d m", n.Scale, maxSide)
	case !within(n.Amplitude, 0, maxHeight):
		return fmt.Errorf("ground noise amplitude %g: 0 to %d m", n.Amplitude, maxHeight)
	case !within(p.Edge.Width, p.Cell, min(p.Size[0], p.Size[1])/2):
		return fmt.Errorf("edge width %g: a cell to half the region's shorter side", p.Edge.Width)
	case !within(p.Edge.Height[0], -maxHeight, maxHeight) || !within(p.Edge.Height[1], p.Edge.Height[0], maxHeight):
		return fmt.Errorf("edge heights %v: low to high, within ±%d m", p.Edge.Height, maxHeight)
	case !reach(p.Colour.Slope, maxHeight) || !within(p.Colour.High, -maxHeight, maxHeight):
		return fmt.Errorf("colour needs a positive slope up to %d and a high mark within ±%d m", maxHeight, maxHeight)
	case len(p.Biomes) == 0:
		return errors.New("needs a biome")
	case len(p.Areas) > maxAreas:
		return fmt.Errorf("%d areas: at most %d", len(p.Areas), maxAreas)
	case !finite(p.Start.Yaw):
		return errors.New("the start needs a yaw")
	}
	if err := p.checkBiome("edge", p.Edge.Biome); err != nil {
		return err
	}
	for _, c := range []string{roadColour, bedColour, waterColour} {
		if _, ok := k.Grounds[c]; !ok {
			return fmt.Errorf("the kit needs a %s ground", c)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(p.Biomes)) {
		if err := p.Biomes[name].check(k); err != nil {
			return fmt.Errorf("biome %s: %w", name, err)
		}
	}
	for i, f := range p.Features {
		if err := p.checkFeature(f); err != nil {
			return fmt.Errorf("feature %d: %w", i+1, err)
		}
	}
	for i, a := range p.Areas {
		if err := p.checkArea(a); err != nil {
			return fmt.Errorf("area %d (%s): %w", i+1, a.ID, err)
		}
	}
	for i, c := range p.Camps {
		if !p.inside(c.At) || !reach(c.Radius, maxReach) {
			return fmt.Errorf("camp %d (%s): needs a spot in the region and a radius up to %d m", i+1, c.Name, maxReach)
		}
	}
	for i, n := range p.NPCs {
		if !finite(n.Yaw) {
			return fmt.Errorf("npc %d (%s): needs a yaw", i+1, n.ID)
		}
	}
	for i, g := range p.Gates {
		if !reach(g.Radius, maxReach) {
			return fmt.Errorf("gate %d (%s): needs a radius up to %d m", i+1, g.Name, maxReach)
		}
	}
	for i, pc := range p.Pieces {
		if err := p.checkPiece(pc, k); err != nil {
			return fmt.Errorf("piece %d: %w", i+1, err)
		}
	}
	for _, s := range p.spots() {
		if !p.inside(s.at) {
			return fmt.Errorf("%s at %v: outside the region", s.what, s.at)
		}
	}
	return nil
}

func (p Plan) checkBiome(what, name string) error {
	if _, ok := p.Biomes[name]; !ok {
		return fmt.Errorf("%s: no biome %q", what, name)
	}
	return nil
}

func (b Biome) check(k Kit) error {
	for _, c := range []string{b.Colours.Flat, b.Colours.Slope, b.Colours.Steep, b.Colours.High} {
		if _, ok := k.Grounds[c]; !ok {
			return fmt.Errorf("colour %q isn't one of the kit's grounds", c)
		}
	}
	for i, s := range b.Scatter {
		a := s.Away
		if _, ok := k.Models[s.Model]; !ok {
			return fmt.Errorf("scatter %d: model %q isn't in the kit", i+1, s.Model)
		}
		if !within(s.Spacing, minSpacing, maxReach) || !within(s.Slope, 0, maxHeight) {
			return fmt.Errorf("scatter %d: a spacing of %d to %d m, and a slope of 0 to %d", i+1, minSpacing, maxReach, maxHeight)
		}
		for _, d := range []float64{a.Road, a.Water, a.Camp, a.Town, a.Waypoint} {
			if !within(d, 0, maxReach) {
				return fmt.Errorf("scatter %d: away distances of 0 to %d m", i+1, maxReach)
			}
		}
	}
	return nil
}

func (p Plan) checkFeature(f Feature) error {
	n := 0
	for _, set := range []bool{f.Ridge != nil, f.Plateau != nil, f.River != nil, f.Lake != nil, f.Road != nil} {
		if set {
			n++
		}
	}
	if n != 1 {
		return errors.New("is one of a ridge, a plateau, a river, a lake or a road")
	}
	switch {
	case f.Ridge != nil:
		r := f.Ridge
		if !p.path(r.Path, 2) || !reach(r.Width, maxReach) || !within(r.Height, -maxHeight, maxHeight) {
			return fmt.Errorf("a ridge needs a path in the region, a width up to %d m and a height within ±%d m", maxReach, maxHeight)
		}
	case f.Plateau != nil:
		pl := f.Plateau
		if !p.path(pl.Area, 3) || !within(pl.Height, -maxHeight, maxHeight) || pl.Height == 0 || (pl.Edge != Cliff && pl.Edge != Ramp) {
			return fmt.Errorf("a plateau needs an area in the region, a height other than 0 within ±%d m, and a cliff or ramp edge", maxHeight)
		}
	case f.River != nil:
		r := f.River
		if !p.path(r.Path, 2) || !reach(r.Width, maxReach) || !reach(r.Depth, maxHeight) || !p.path(r.Fords, 0) {
			return fmt.Errorf("a river needs a path and fords in the region, a width up to %d m and a depth up to %d m", maxReach, maxHeight)
		}
	case f.Lake != nil:
		if !p.path(f.Lake.Area, 3) || !reach(f.Lake.Depth, maxHeight) {
			return fmt.Errorf("a lake needs an area in the region and a depth up to %d m", maxHeight)
		}
	default:
		if !p.path(f.Road.Path, 2) || !reach(f.Road.Width, maxReach) {
			return fmt.Errorf("a road needs a path in the region and a width up to %d m", maxReach)
		}
	}
	return nil
}

func (p Plan) checkArea(a Area) error {
	if a.ID == "" || !p.path(a.Area, 3) {
		return errors.New("needs an id and an area in the region")
	}
	if a.Kind != Hunting && a.Kind != Town {
		return fmt.Errorf("kind %q: hunting or town", a.Kind)
	}
	if b := a.Boss; b != nil && (!p.inside(b.At) || !reach(b.Radius, maxReach)) {
		return fmt.Errorf("its boss needs a spot in the region and a radius up to %d m", maxReach)
	}
	return p.checkBiome("its biome", a.Biome)
}

func (p Plan) checkPiece(pc Piece, k Kit) error {
	switch {
	case pc.House != nil && pc.Model == "" && pc.At == nil:
		h := pc.House
		if _, ok := k.Models[h.Model]; !ok || !reach(h.Area[2], maxReach) || !reach(h.Area[3], maxReach) || !reach(h.Height, maxHeight) ||
			!p.path([][2]float64{{h.Area[0], h.Area[1]}, {h.Area[0] + h.Area[2], h.Area[1] + h.Area[3]}}, 2) {
			return fmt.Errorf("a house needs a kit model, an area in the region up to %d m a side, and a height up to %d m", maxReach, maxHeight)
		}
	case pc.House == nil && pc.At != nil:
		if _, ok := k.Models[pc.Model]; !ok || !p.inside(*pc.At) || !finite(pc.Yaw) {
			return errors.New("needs a kit model, a spot in the region and a yaw")
		}
	default:
		return errors.New("is either a model at a spot or a house")
	}
	return nil
}

// spot is a point scatter keeps clear of, with how far its own footprint reaches.
type spot struct {
	what   string
	at     [2]float64
	radius float64
}

// spotRadius is the ground a waypoint, an NPC or the start keeps clear.
const spotRadius = 2

// spots are the plan's waypoints, NPCs, gates, pieces and start, in that order.
func (p Plan) spots() []spot {
	var s []spot
	for _, w := range p.Waypoints {
		s = append(s, spot{"waypoint " + w.ID, w.At, spotRadius})
	}
	for _, n := range p.NPCs {
		s = append(s, spot{"npc " + n.ID, n.At, spotRadius})
	}
	for _, g := range p.Gates {
		s = append(s, spot{"gate " + g.Name, g.At, max(g.Radius, spotRadius)})
	}
	for _, pc := range p.Pieces {
		if pc.At != nil {
			s = append(s, spot{"piece " + pc.Model, *pc.At, spotRadius})
		}
	}
	return append(s, spot{"the start", p.Start.At, spotRadius})
}

// inside is whether at lies on the region, its edges included.
func (p Plan) inside(at [2]float64) bool {
	return at[0] >= 0 && at[0] <= p.Size[0] && at[1] >= 0 && at[1] <= p.Size[1]
}

// samples is how many grid points span length at cell apart, cell dividing it.
func samples(length, cell float64) int { return int(length/cell) + 1 }

// whole is whether v is a whole number of at least 1.
func whole(v float64) bool { return v >= 1 && v == math.Floor(v) }

// path is whether pts has at least n points, each in the region.
func (p Plan) path(pts [][2]float64, n int) bool {
	if len(pts) < n {
		return false
	}
	for _, at := range pts {
		if !p.inside(at) {
			return false
		}
	}
	return true
}

// within is whether x lies from lo to hi; NaN never does.
func within(x, lo, hi float64) bool { return x >= lo && x <= hi }

// reach is whether x is above 0 and at most most.
func reach(x, most float64) bool { return x > 0 && x <= most }

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
