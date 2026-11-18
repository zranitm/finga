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

// check refuses a plan the generator can't build with kit k; the loader checks how its parts fit together.
func (p Plan) check(k Kit) error {
	nx, okX := samples(p.Size[0], p.Cell)
	nz, okZ := samples(p.Size[1], p.Cell)
	switch {
	case !positive(p.Size[0]) || !positive(p.Size[1]):
		return fmt.Errorf("size %v: both sides must be positive", p.Size)
	case !positive(p.Cell) || !okX || !okZ:
		return fmt.Errorf("cell %g must be positive and divide the size %g × %g", p.Cell, p.Size[0], p.Size[1])
	case nx*nz > 1<<24:
		return fmt.Errorf("%d × %d terrain samples: at most 2²⁴", nx, nz)
	case !whole(p.Size[0]/tileM) || !whole(p.Size[1]/tileM) || !whole(overviewM/p.Cell):
		return fmt.Errorf("size %v must be whole %d m map tiles, so whole %d m chunks, and cell %g divide the overview's %d m", p.Size, tileM, chunkM, p.Cell, overviewM)
	case p.Look.Sky == "":
		return errors.New("look needs a sky")
	case !positive(p.Ground.Noise.Scale) || !finite(p.Ground.Noise.Amplitude) || p.Ground.Noise.Amplitude < 0 || p.Ground.Noise.Octaves < 1 || p.Ground.Noise.Octaves > 8:
		return errors.New("ground noise needs a positive scale, an amplitude of 0 or more, and 1 to 8 octaves")
	case !positive(p.Edge.Width) || !finite(p.Edge.Height[0]) || !finite(p.Edge.Height[1]) || p.Edge.Height[0] > p.Edge.Height[1]:
		return errors.New("edge needs a positive width and its heights, low to high")
	case !positive(p.Colour.Slope) || !finite(p.Colour.High):
		return errors.New("colour needs a positive slope and a high mark")
	case len(p.Biomes) == 0:
		return errors.New("needs a biome")
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
		if err := f.check(); err != nil {
			return fmt.Errorf("feature %d: %w", i+1, err)
		}
	}
	for i, a := range p.Areas {
		if err := p.checkArea(a); err != nil {
			return fmt.Errorf("area %d (%s): %w", i+1, a.ID, err)
		}
	}
	for i, c := range p.Camps {
		if !p.inside(c.At) || !positive(c.Radius) {
			return fmt.Errorf("camp %d (%s): needs a spot in the region and a positive radius", i+1, c.Name)
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
		if !positive(s.Spacing) || !nonNegative(s.Slope) || !nonNegative(a.Road) || !nonNegative(a.Water) || !nonNegative(a.Camp) || !nonNegative(a.Town) || !nonNegative(a.Waypoint) {
			return fmt.Errorf("scatter %d: needs a positive spacing, and a slope and distances of 0 or more", i+1)
		}
	}
	return nil
}

func (f Feature) check() error {
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
		if !path(r.Path) || !positive(r.Width) || !finite(r.Height) {
			return errors.New("a ridge needs a path, a positive width and a height")
		}
	case f.Plateau != nil:
		pl := f.Plateau
		if !polygon(pl.Area) || !finite(pl.Height) || (pl.Edge != Cliff && pl.Edge != Ramp) {
			return errors.New("a plateau needs an area, a height, and a cliff or ramp edge")
		}
	case f.River != nil:
		r := f.River
		if !path(r.Path) || !positive(r.Width) || !positive(r.Depth) {
			return errors.New("a river needs a path, a positive width and a positive depth")
		}
		for _, at := range r.Fords {
			if !finite(at[0]) || !finite(at[1]) {
				return errors.New("a ford needs a spot")
			}
		}
	case f.Lake != nil:
		if !polygon(f.Lake.Area) || !positive(f.Lake.Depth) {
			return errors.New("a lake needs an area and a positive depth")
		}
	default:
		if !path(f.Road.Path) || !positive(f.Road.Width) {
			return errors.New("a road needs a path and a positive width")
		}
	}
	return nil
}

func (p Plan) checkArea(a Area) error {
	if a.ID == "" || !polygon(a.Area) {
		return errors.New("needs an id and an area")
	}
	if a.Kind != Hunting && a.Kind != Town {
		return fmt.Errorf("kind %q: hunting or town", a.Kind)
	}
	return p.checkBiome("its biome", a.Biome)
}

func (p Plan) checkPiece(pc Piece, k Kit) error {
	switch {
	case pc.House != nil && pc.Model == "" && pc.At == nil:
		h := pc.House
		if _, ok := k.Models[h.Model]; !ok || !p.inside([2]float64{h.Area[0], h.Area[1]}) || !positive(h.Area[2]) || !positive(h.Area[3]) || !positive(h.Height) {
			return errors.New("a house needs a kit model, an area of positive width and depth starting in the region, and a positive height")
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

// samples is how many grid points span length at cell apart, and whether cell divides it.
func samples(length, cell float64) (int, bool) {
	if !positive(length) || !positive(cell) {
		return 0, false
	}
	k := math.Floor(length/cell + 0.5)
	return int(k) + 1, k >= 1 && k < 1<<24 && math.Abs(float64(k*cell)-length) < 1e-9
}

// whole is whether v is a whole number of at least 1.
func whole(v float64) bool { return v >= 1 && v == math.Floor(v) }

func path(pts [][2]float64) bool { return len(pts) >= 2 && allFinite(pts) }

func polygon(pts [][2]float64) bool { return len(pts) >= 3 && allFinite(pts) }

func allFinite(pts [][2]float64) bool {
	for _, p := range pts {
		if !finite(p[0]) || !finite(p[1]) {
			return false
		}
	}
	return true
}

// YAML can spell NaN and infinity, which pass a check like x <= 0; these refuse them.
func positive(x float64) bool { return x > 0 && !math.IsInf(x, 1) }

func nonNegative(x float64) bool { return x >= 0 && !math.IsInf(x, 1) }

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }
