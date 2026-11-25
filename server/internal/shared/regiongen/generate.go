package regiongen

import (
	"maps"
	"math"
	"slices"

	"finga/internal/shared/sim"
)

// Region is a plan built: the geometry the sim moves through, and the colours, props and water the client draws.
type Region struct {
	Plan     Plan
	Geometry sim.Geometry // indexed; every number in it a float32's, as the bake stores it
	Colours  []uint8      // per terrain sample, an index into Palette
	Palette  []string     // the kit's grounds, by name
	RGB      []uint32     // each palette colour, 0xrrggbb
	Models   []string     // the kit's models, by name; a prop's Model indexes them
	Props    []Prop       // set pieces, then scattered
	Rivers   []RiverWater
	Lakes    []LakeWater
	Flames   []Flame    // a cave's torches' lights, in the props' order
	Wet      []bool     // per terrain sample: under a river or a lake
	Heights  [2]float64 // the lowest and highest sample
}

// Prop is a model drawn at a spot, turned Yaw degrees from north toward east.
type Prop struct {
	Model int
	X, Z  float64
	Yaw   float64
}

// RiverWater is a river's surface: Width across along its smoothed Path, at Levels by its points, falling downstream.
type RiverWater struct {
	Path   [][2]float64
	Width  float64
	Levels []float64
}

// LakeWater is a lake's surface: flat at Level over Area.
type LakeWater struct {
	Area  [][2]float64
	Level float64
}

const (
	chunkM     = 64  // a bake chunk's side
	overviewM  = 8   // the overview's spacing
	smoothing  = 3   // Chaikin passes on a river's or a road's path
	freeboard  = 0.5 // a river's or a lake's water below its lowest bank
	fordDepth  = 0.3 // a ford's water
	fordRamp   = 0.5 // how far the ground round a ford rises a metre, under max_slope's 0.7, so a body walks out of it every way
	wadeDepth  = 0.6 // water deeper than this is a wall to every body: twice a ford's
	roadMean   = 15  // a road's height is the ground's mean this far along it each way
	townBlend  = 12  // a town's flat ground eases into the land around over this
	cliffCells = 2   // a cliff edge falls over this many samples
)

// The kit grounds roads and wet ground take.
const (
	roadColour  = "dirt"
	bedColour   = "sand"
	waterColour = "water" // on the map, over a bed
)

// builder is a region on its way: the heights and every mark the later steps read.
type builder struct {
	p        Plan
	k        Kit
	g        grid
	maxSlope float64
	radius   float64 // a body's
	h        []float64
	area     []int16   // per sample, the plan's area holding it, or -1
	onRoad   []bool    // per sample: under a road
	water    []float64 // per sample: the highest water surface over it, -Inf where none
	f        *field
	roads    []Road    // smoothed
	rivers   []River   // smoothed
	floor    []float64 // a cave's ground under its rock, which its halls and tunnels are carved down to
	rocky    []bool    // a cave's, per sample: rock or a wall, not carved floor
	past     []float64 // a cave's, per sample: the least distance past a carved space's wandering line, negative inside one
	flat     []float64 // per feature: a flat hall's level, NaN for any other
	tunnels  []carved  // a cave's tunnels carved so far
	wander   *noise    // a cave's: how its carved walls' lines wander
	r        Region
}

// Generate builds p with kit k for the sim's body, its slope and radius, the same bits on every CPU.
func Generate(p Plan, k Kit, body sim.Params) (*Region, error) {
	if err := p.check(k, body.Radius); err != nil {
		return nil, err
	}
	maxSlope := body.MaxSlope
	nx, nz := samples(p.Size[0], p.Cell), samples(p.Size[1], p.Cell)
	g := grid{nx: nx, nz: nz, cell: p.Cell}
	b := &builder{p: p, k: k, g: g, maxSlope: maxSlope, radius: body.Radius, h: make([]float64, nx*nz), onRoad: make([]bool, nx*nz), water: make([]float64, nx*nz), f: newField(nx * nz)}
	for i := range b.water {
		b.water[i] = math.Inf(-1)
	}
	b.r = Region{Plan: p, Wet: make([]bool, nx*nz), Palette: k.palette(), Models: slices.Sorted(maps.Keys(k.Models))}
	for _, name := range b.r.Palette {
		b.r.RGB = append(b.r.RGB, k.rgbOf(name))
	}
	b.areas()
	b.ground()
	b.rock()
	for i, f := range p.Features {
		switch {
		case f.Ridge != nil:
			b.ridge(*f.Ridge)
		case f.Plateau != nil:
			b.plateau(*f.Plateau)
		case f.River != nil:
			b.river(*f.River)
		case f.Lake != nil:
			b.lake(*f.Lake)
		case f.Hall != nil:
			b.hall(i, *f.Hall)
		case f.Tunnel != nil:
			b.tunnel(*f.Tunnel)
		default:
			b.road(*f.Road)
		}
	}
	b.towns()
	b.edge()
	lo, hi := math.Inf(1), math.Inf(-1)
	for i, v := range b.h {
		b.h[i] = f32(v)
		lo, hi = min(lo, b.h[i]), max(hi, b.h[i])
	}
	b.r.Heights = [2]float64{lo, hi}
	t := sim.NewTerrain(p.Cell, nx, nz, b.h, maxSlope)
	t.DeepWater = make([]bool, nx*nz)
	for s, wet := range b.r.Wet { // judged on the final ground, which roads, towns and the edge reshape after the water
		t.DeepWater[s] = wet && b.water[s]-b.h[s] > wadeDepth || b.walled(s)
	}
	steep := b.colours(t)
	var placed solidsNear
	b.r.Geometry = sim.Geometry{SizeX: p.Size[0], SizeZ: p.Size[1], Terrain: t, Boxes: []sim.Box{}, Cylinders: []sim.Cylinder{}}
	b.pieces(&placed)
	b.torches(&placed)
	b.scatter(steep, &placed)
	b.flames()
	b.r.Geometry = sim.Indexed(b.r.Geometry)
	return &b.r, nil
}

// areas marks each sample with the first of the plan's areas holding it.
func (b *builder) areas() {
	b.area = make([]int16, len(b.h))
	for i := range b.area {
		b.area[i] = -1
	}
	for i, a := range b.p.Areas {
		b.g.fill(a.Area, func(s int) {
			if b.area[s] < 0 {
				b.area[s] = int16(i) //nolint:gosec // a plan has far fewer than 2¹⁵ areas
			}
		})
	}
}

// ground is the plan's noise: octaves, each half the last's size and weight, scaled to about ±amplitude.
func (b *builder) ground() {
	n := b.p.Ground.Noise
	octaves := make([]*noise, n.Octaves)
	total, w := 0.0, 1.0
	for k := range octaves {
		octaves[k] = newNoise(int64(b.p.Seed) + int64(k))
		total, w = total+w, w/2
	}
	for iz := range b.g.nz {
		for ix := range b.g.nx {
			x, z := b.g.x(ix), b.g.x(iz)
			sum, weight, scale := 0.0, 1.0, n.Scale
			for _, o := range octaves {
				sum += float64(weight * o.at(x/scale, z/scale))
				weight, scale = weight/2, scale/2
			}
			b.h[b.g.index(ix, iz)] = float64(n.Amplitude * (sum / total))
		}
	}
}

// edgeCliff is how far the ring of mountains' inner face rises over the highest ground inside it: past a jump from a tree's top with a leap at its peak, so nobody gets onto the ring.
const edgeCliff = 10

// edge raises the ring of mountains last, so no feature or town opens it: a cliff edgeCliff over the highest ground inside it at its inner face, rising by between its heights to the sides, noise choosing where along them.
func (b *builder) edge() {
	e := b.p.Edge
	side := func(s int) float64 {
		x, z := b.g.x(s%b.g.nx), b.g.x(s/b.g.nx)
		return min(x, z, b.p.Size[0]-x, b.p.Size[1]-z)
	}
	top := math.Inf(-1)
	for s, h := range b.h {
		if side(s) >= e.Width {
			top = max(top, h)
		}
	}
	n := newNoise(int64(b.p.Seed) + 100)
	for s := range b.h {
		d := side(s)
		if d >= e.Width {
			continue
		}
		x, z := b.g.x(s%b.g.nx), b.g.x(s/b.g.nx)
		pick := min(max((n.at(x/e.Width, z/e.Width)/0.87+1)/2, 0), 1)
		b.h[s] = top + edgeCliff + float64(smoothstep(1-d/e.Width)*lerp(e.Height[0], e.Height[1], pick))
		b.r.Wet[s] = false
	}
}

// ridge raises Height along its path, (1 − d²/r²)² of it at d from the path, r half its width.
func (b *builder) ridge(r Ridge) {
	half := float64(r.Width / 2)
	b.f.near(b.g, r.Path, false, half)
	r2 := float64(half * half)
	for _, s := range b.f.touched {
		w := 1 - b.f.d2[s]/r2
		b.h[s] += float64(r.Height * float64(w*w))
	}
}

// plateau raises Height over its area, falling straight to nothing outside it: over two samples for a cliff, at half the walkable slope for a ramp.
func (b *builder) plateau(p Plateau) {
	edge := float64(cliffCells * b.g.cell)
	if p.Edge == Ramp {
		edge = math.Abs(p.Height) / (b.maxSlope / 2)
	}
	b.f.near(b.g, p.Area, true, edge)
	inside := b.inside(p.Area)
	for _, s := range b.f.touched {
		if !inside[s] {
			b.h[s] += float64(p.Height * (1 - math.Sqrt(b.f.d2[s])/edge))
		}
	}
	for s, in := range inside {
		if in {
			b.h[s] += p.Height
		}
	}
}

// inside marks the samples within poly.
func (b *builder) inside(poly [][2]float64) []bool {
	in := make([]bool, len(b.h))
	b.g.fill(poly, func(s int) { in[s] = true })
	return in
}

// bank is how wide a river's or a lake's banks are: steep to its full depth, gentle to a ford's.
func bank(width float64) float64 { return min(width/4, 2) }

// river carves its bed along its smoothed path: Depth below water that sits freeboard under the lowest bank yet, between banks that fall to it; shallow at fords, the ground round each sloping down to it no steeper than fordRamp.
func (b *builder) river(r River) {
	path := smooth(r.Path)
	levels := make([]float64, len(path))
	for i, p := range path {
		levels[i] = b.g.height(b.h, p[0], p[1]) - freeboard
		if i > 0 {
			levels[i] = min(levels[i], levels[i-1])
		}
		levels[i] = f32(levels[i])
	}
	r.Path = path
	b.rivers = append(b.rivers, r)
	b.r.Rivers = append(b.r.Rivers, RiverWater{Path: f32Path(path), Width: f32(r.Width), Levels: levels})
	half, bk := float64(r.Width/2), bank(r.Width)
	b.f.near(b.g, path, false, half)
	fordR := half + bk
	fordBeds := make([]float64, len(r.Fords))
	for i := range fordBeds {
		fordBeds[i] = math.Inf(1)
	}
	for _, s := range b.f.touched {
		level := lerp(levels[b.f.seg[s]], levels[b.f.seg[s]+1], b.f.t[s])
		depth := r.Depth
		x, z := b.g.x(int(s)%b.g.nx), b.g.x(int(s)/b.g.nx)
		for i, at := range r.Fords {
			if dx, dz := x-at[0], z-at[1]; float64(dx*dx)+float64(dz*dz) < float64(fordR*fordR) {
				depth = fordDepth
				fordBeds[i] = min(fordBeds[i], level-fordDepth)
			}
		}
		b.carve(int(s), level-depth, (math.Sqrt(b.f.d2[s])-(half-bk))/bk)
		b.r.Wet[s], b.water[s] = true, max(b.water[s], level)
	}
	for i, at := range r.Fords {
		b.ramp(at, fordBeds[i], half-bk)
	}
}

// ramp holds the ground round a ford at at under a cone: flat at bed within floor of it, then rising fordRamp a metre, so the river's banks there fall no steeper and a body walks into the ford and out of it every way.
func (b *builder) ramp(at [2]float64, bed, floor float64) {
	for s := range b.h {
		dx, dz := b.g.x(s%b.g.nx)-at[0], b.g.x(s/b.g.nx)-at[1]
		d := math.Sqrt(float64(dx*dx) + float64(dz*dz))
		b.h[s] = min(b.h[s], bed+float64(fordRamp*max(d-floor, 0)))
	}
}

// carve lowers sample s to bed, or up a bank toward the ground at share up of its way.
func (b *builder) carve(s int, bed, up float64) {
	b.h[s] = min(b.h[s], lerp(bed, b.h[s], min(max(up, 0), 1)))
}

// lake carves its area to Depth below water freeboard under the lowest ground along its edge, banks falling to it inside the edge.
func (b *builder) lake(l Lake) {
	level := math.Inf(1)
	for i, a := range l.Area {
		c := l.Area[(i+1)%len(l.Area)]
		for _, t := range [...]float64{0, 0.25, 0.5, 0.75} {
			p := lerp2(a, c, t)
			level = min(level, b.g.height(b.h, p[0], p[1]))
		}
	}
	level = f32(level - freeboard)
	b.r.Lakes = append(b.r.Lakes, LakeWater{Area: f32Path(l.Area), Level: level})
	bk := bank(l.Depth * 4)
	b.f.near(b.g, l.Area, true, bk)
	for s, in := range b.inside(l.Area) {
		if !in {
			continue
		}
		up := 0.0
		if b.f.has(s) {
			up = 1 - math.Sqrt(b.f.d2[s])/bk
		}
		b.carve(s, level-l.Depth, up)
		b.r.Wet[s], b.water[s] = true, max(b.water[s], level)
	}
}

// road flattens its smoothed path to the ground's mean along it, roadMean each way, easing back to the land over a shoulder as wide as the road; across water it's a causeway.
func (b *builder) road(r Road) {
	path := smooth(r.Path)
	s := along(path)
	ground := make([]float64, len(path))
	for i, p := range path {
		ground[i] = b.g.height(b.h, p[0], p[1])
	}
	levels := make([]float64, len(path))
	for i := range path {
		sum, n := 0.0, 0
		for j := range path {
			if math.Abs(s[j]-s[i]) <= roadMean {
				sum, n = sum+ground[j], n+1
			}
		}
		levels[i] = sum / float64(n)
	}
	r.Path = path
	b.roads = append(b.roads, r)
	half := float64(r.Width / 2)
	b.f.near(b.g, path, false, half+r.Width)
	for _, t := range b.f.touched {
		level := lerp(levels[b.f.seg[t]], levels[b.f.seg[t]+1], b.f.t[t])
		d := math.Sqrt(b.f.d2[t])
		b.h[t] = lerp(level, b.h[t], smoothstep((d-half)/r.Width))
		if d <= half { // a road over water is a causeway
			b.onRoad[t], b.r.Wet[t] = true, false
		}
	}
}

// towns flattens each town to its mean height, easing into the land around over townBlend.
func (b *builder) towns() {
	for _, a := range b.p.Areas {
		if a.Kind != Town {
			continue
		}
		in := b.inside(a.Area)
		sum, n := 0.0, 0
		for s, ok := range in {
			if ok {
				sum, n = sum+b.h[s], n+1
			}
		}
		if n == 0 {
			continue
		}
		mean := sum / float64(n)
		b.f.near(b.g, a.Area, true, townBlend)
		for _, s := range b.f.touched {
			if !in[s] {
				b.h[s] = lerp(mean, b.h[s], smoothstep(math.Sqrt(b.f.d2[s])/townBlend))
			}
		}
		for s, ok := range in {
			if ok {
				b.h[s] = mean
			}
		}
	}
}

func f32Path(path [][2]float64) [][2]float64 {
	out := make([][2]float64, len(path))
	for i, p := range path {
		out[i] = [2]float64{f32(p[0]), f32(p[1])}
	}
	return out
}
