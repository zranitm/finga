package regiongen

import (
	"maps"
	"math"
	"math/rand/v2"
	"slices"

	"finga/internal/shared/mapfile"
	"finga/internal/shared/sim"
)

// colours paints each sample: a road's dirt, a bed's sand, a cave's rock and its walls cave_rock, or else its biome's colour for ground too steep to walk (a cell touching it marked steep), above the plan's high mark, sloped past its slope, or flat. It returns the steep samples.
func (b *builder) colours(t sim.Terrain) []bool {
	index := map[string]uint8{}
	for i, name := range b.r.Palette {
		index[name] = uint8(i) //nolint:gosec // DecodeKit caps grounds at 256
	}
	steep := make([]bool, len(b.h))
	b.r.Colours = make([]uint8, len(b.h))
	slope2 := float64(b.p.Colour.Slope * b.p.Colour.Slope)
	for iz := range b.g.nz {
		for ix := range b.g.nx {
			s := b.g.index(ix, iz)
			steep[s] = b.steepAt(t, ix, iz)
			c := b.p.Biomes[b.biomeOf(s)].Colours
			name := c.Flat
			switch {
			case b.onRoad[s]:
				name = roadColour
			case b.r.Wet[s]:
				name = bedColour
			case b.rocky != nil && b.rocky[s]:
				name = rockColour
			case steep[s]:
				name = c.Steep
			case b.h[s] >= b.p.Colour.High:
				name = c.High
			case b.slope2(ix, iz) >= slope2:
				name = c.Slope
			}
			b.r.Colours[s] = index[name]
		}
	}
	return steep
}

// biomeOf is the biome of sample s: its area's, or the edge's outside every area.
func (b *builder) biomeOf(s int) string {
	if a := b.area[s]; a >= 0 {
		return b.p.Areas[a].Biome
	}
	return b.p.Edge.Biome
}

// steepAt is whether any cell touching sample (ix, iz) is too steep to walk along either axis.
func (b *builder) steepAt(t sim.Terrain, ix, iz int) bool {
	for _, c := range [...][2]int{{ix - 1, iz - 1}, {ix, iz - 1}, {ix - 1, iz}, {ix, iz}} {
		if c[0] < 0 || c[1] < 0 || c[0] >= b.g.nx-1 || c[1] >= b.g.nz-1 {
			continue
		}
		x, z := float64((float64(c[0])+0.5)*b.g.cell), float64((float64(c[1])+0.5)*b.g.cell)
		if t.Steep(x, z, false) || t.Steep(x, z, true) {
			return true
		}
	}
	return false
}

// slope2 is the squared rise per metre at sample (ix, iz), across its neighbours.
func (b *builder) slope2(ix, iz int) float64 {
	x0, x1 := b.g.clampX(ix-1), b.g.clampX(ix+1)
	z0, z1 := b.g.clampZ(iz-1), b.g.clampZ(iz+1)
	gx := (b.h[b.g.index(x1, iz)] - b.h[b.g.index(x0, iz)]) / float64(float64(x1-x0)*b.g.cell)
	gz := (b.h[b.g.index(ix, z1)] - b.h[b.g.index(ix, z0)]) / float64(float64(z1-z0)*b.g.cell)
	return float64(gx*gx) + float64(gz*gz)
}

// The waypoint stone's solid (large-regions.md section 8): its obelisk, as the crystal floats above it out of reach.
const (
	stoneRadius = 0.5
	stoneHeight = 2.82
)

// pieces places the plan's set pieces as written, each with its kit model's solid, and each waypoint's stone, a solid only, as the client draws the stone lit or dim.
func (b *builder) pieces(placed *solidsNear) {
	for _, w := range b.p.Waypoints {
		b.cylinder(f32(w.At[0]), f32(w.At[1]), stoneRadius, stoneHeight, placed)
	}
	for _, pc := range b.p.Pieces {
		if h := pc.House; h != nil {
			x, z := f32(h.Area[0]+float64(h.Area[2]/2)), f32(h.Area[1]+float64(h.Area[3]/2))
			b.prop(h.Model, x, z, 0)
			b.box(x, z, h.Area[2]/2, h.Area[3]/2, 0, h.Height, placed)
			continue
		}
		b.place(pc.Model, f32(pc.At[0]), f32(pc.At[1]), f32(pc.Yaw), placed)
	}
}

// place draws model at (x, z) turned yaw, with its solid.
func (b *builder) place(model string, x, z, yaw float64, placed *solidsNear) {
	b.prop(model, x, z, yaw)
	switch s := b.k.Models[model]; {
	case s.Cylinder != nil:
		b.cylinder(x, z, s.Cylinder.Radius, s.Cylinder.Height, placed)
	case s.Box != nil:
		b.box(x, z, s.Box.Width/2, s.Box.Depth/2, yaw, s.Box.Height, placed)
	}
}

func (b *builder) prop(model string, x, z, yaw float64) {
	m, _ := slices.BinarySearch(b.r.Models, model)
	b.r.Props = append(b.r.Props, Prop{Model: m, X: x, Z: z, Yaw: yaw})
}

// cylinder adds a cylinder solid of radius r at (x, z), height above the ground there.
func (b *builder) cylinder(x, z, r, height float64, placed *solidsNear) {
	r = f32(r)
	g := &b.r.Geometry
	g.Cylinders = append(g.Cylinders, sim.Cylinder{X: x, Z: z, Radius: r, Top: f32(g.Terrain.Height(x, z) + height)})
	placed.add(x, z, r)
}

// box adds a box solid at (x, z), halfW across its yaw and halfD along it, height above the ground at its centre.
func (b *builder) box(x, z, halfW, halfD, yaw, height float64, placed *solidsNear) {
	ux, uz := mapfile.Axis(yaw)
	g := &b.r.Geometry
	g.Boxes = append(g.Boxes, sim.Box{X: x, Z: z, HalfW: f32(halfW), HalfD: f32(halfD), UX: f32(ux), UZ: f32(uz), Top: f32(g.Terrain.Height(x, z) + height)})
	placed.add(x, z, math.Sqrt(float64(halfW*halfW)+float64(halfD*halfD)))
}

// reach is how far a model's solid reaches from its centre, 0 for looks only.
func (s Solid) reach() float64 {
	switch {
	case s.Cylinder != nil:
		return s.Cylinder.Radius
	case s.Box != nil:
		return math.Sqrt(float64(s.Box.Width*s.Box.Width)+float64(s.Box.Depth*s.Box.Depth)) / 2
	}
	return 0
}

// tries is how many ring points a disc tries round an active point before it's done.
const tries = 30

// scatter strews each biome's models, biome by biome in name order, over the ground the biome holds: Poisson disc points, kept where the ground and every away distance allow and no solid already stands.
func (b *builder) scatter(steep []bool, placed *solidsNear) {
	clear := b.clearances()
	stream := uint64(0)
	for _, name := range slices.Sorted(maps.Keys(b.p.Biomes)) {
		lo, hi, ok := b.bounds(name)
		for _, sc := range b.p.Biomes[name].Scatter {
			stream++
			if !ok {
				continue
			}
			rng := rand.NewPCG(uint64(b.p.Seed), stream)
			solid := b.k.Models[sc.Model]
			r := solid.reach()
			a := sc.Away
			slope2 := float64(sc.Slope * sc.Slope)
			for _, pt := range poisson(rng, lo, hi, sc.Spacing) {
				x, z := f32(pt[0]), f32(pt[1])
				s := b.g.nearest(x, z)
				ix, iz := s%b.g.nx, s/b.g.nx
				switch {
				case b.biomeOf(s) != name || steep[s] || (sc.Slope > 0 && b.slope2(ix, iz) > slope2) || (b.rocky != nil && b.rocky[s]):
					continue
				case x < r || z < r || x > b.p.Size[0]-r || z > b.p.Size[1]-r:
					continue
				case !clear.allow(s, a, r) || placed.overlaps(x, z, r):
					continue
				}
				yaw := float64(rng.Uint64() % 360)
				if solid.Box != nil {
					yaw = float64(90 * (rng.Uint64() % 4)) // a box's axis stays exact
				}
				b.place(sc.Model, x, z, yaw, placed)
			}
		}
	}
}

// bounds is the box round the samples of biome name, and whether it has any.
func (b *builder) bounds(name string) ([2]float64, [2]float64, bool) {
	lo, hi := [2]float64{math.Inf(1), math.Inf(1)}, [2]float64{math.Inf(-1), math.Inf(-1)}
	for iz := range b.g.nz {
		for ix := range b.g.nx {
			if b.biomeOf(b.g.index(ix, iz)) == name {
				x, z := b.g.x(ix), b.g.x(iz)
				lo, hi = [2]float64{min(lo[0], x), min(lo[1], z)}, [2]float64{max(hi[0], x), max(hi[1], z)}
			}
		}
	}
	return lo, hi, lo[0] <= hi[0]
}

// poisson is Bridson's blue noise over the box lo to hi: points r apart at least, each new one tried in the ring r to 2r round an active point, found by rejection in a square so no angle is needed.
func poisson(rng *rand.PCG, lo, hi [2]float64, r float64) [][2]float64 {
	side := float64(r * 0.7071067811865476) // r/√2: a grid square holds one point at most
	gw, gh := int(math.Floor((hi[0]-lo[0])/side))+1, int(math.Floor((hi[1]-lo[1])/side))+1
	cells := make([]int32, gw*gh)
	for i := range cells {
		cells[i] = -1
	}
	cellOf := func(p [2]float64) (int, int) {
		return min(int(math.Floor((p[0]-lo[0])/side)), gw-1), min(int(math.Floor((p[1]-lo[1])/side)), gh-1)
	}
	r2, outer2 := float64(r*r), float64(4*float64(r*r))
	var pts [][2]float64
	add := func(p [2]float64) {
		cx, cz := cellOf(p)
		cells[cz*gw+cx] = int32(len(pts)) //nolint:gosec // a region holds far fewer than 2³¹ points
		pts = append(pts, p)
	}
	fits := func(p [2]float64) bool {
		if p[0] < lo[0] || p[1] < lo[1] || p[0] > hi[0] || p[1] > hi[1] {
			return false
		}
		cx, cz := cellOf(p)
		for z := max(cz-2, 0); z <= min(cz+2, gh-1); z++ {
			for x := max(cx-2, 0); x <= min(cx+2, gw-1); x++ {
				if k := cells[z*gw+x]; k >= 0 {
					if dx, dz := p[0]-pts[k][0], p[1]-pts[k][1]; float64(dx*dx)+float64(dz*dz) < r2 {
						return false
					}
				}
			}
		}
		return true
	}
	add([2]float64{lerp(lo[0], hi[0], unit(rng)), lerp(lo[1], hi[1], unit(rng))})
	active := []int{0}
	for len(active) > 0 {
		k := int(rng.Uint64() % uint64(len(active)))
		at := pts[active[k]]
		found := false
		for t := 0; t < tries && !found; {
			dx, dz := float64((2*unit(rng)-1)*(2*r)), float64((2*unit(rng)-1)*(2*r))
			if d2 := float64(dx*dx) + float64(dz*dz); d2 < r2 || d2 > outer2 {
				continue
			}
			t++
			if p := [2]float64{at[0] + dx, at[1] + dz}; fits(p) {
				add(p)
				active = append(active, len(pts)-1)
				found = true
			}
		}
		if !found {
			active[k] = active[len(active)-1]
			active = active[:len(active)-1]
		}
	}
	return pts
}

// unit is a draw from 0 up to 1, exact: 53 random bits.
func unit(rng *rand.PCG) float64 { return float64(rng.Uint64()>>11) * 0x1p-53 }

// solidsNear holds placed solids as circles in 8 m squares, so a new one tests only its neighbours.
type solidsNear struct {
	in   map[[2]int][][3]float64
	wide float64 // the widest reach placed
}

const solidsSquare = 8

func (n *solidsNear) square(x, z float64) [2]int {
	return [2]int{int(math.Floor(x / solidsSquare)), int(math.Floor(z / solidsSquare))}
}

func (n *solidsNear) add(x, z, r float64) {
	if r <= 0 {
		return
	}
	if n.in == nil {
		n.in = map[[2]int][][3]float64{}
	}
	q := n.square(x, z)
	n.in[q] = append(n.in[q], [3]float64{x, z, r})
	n.wide = max(n.wide, r)
}

// solidGap is the room kept between scattered solids.
const solidGap = 0.5

// overlaps is whether a solid reaching r from (x, z) would come within solidGap of one placed; looks only never does.
func (n *solidsNear) overlaps(x, z, r float64) bool {
	if r <= 0 {
		return false
	}
	q := n.square(x, z)
	reach := int(math.Ceil((r+solidGap+n.wide)/solidsSquare)) + 1
	for dz := -reach; dz <= reach; dz++ {
		for dx := -reach; dx <= reach; dx++ {
			for _, c := range n.in[[2]int{q[0] + dx, q[1] + dz}] {
				ex, ez, gap := x-c[0], z-c[1], r+c[2]+solidGap
				if float64(ex*ex)+float64(ez*ez) < float64(gap*gap) {
					return true
				}
			}
		}
	}
	return false
}

// clearances are, per sample, how far it lies past the edge of the nearest road, water, camp, town and spot (waypoints, NPCs, gates, pieces, the start), 0 on one, up to a band past which none matters.
type clearances struct {
	road, water, camp, town, spot []float32
}

// allow is whether a solid reaching r stands at sample s clear of everything by a's distances.
func (c clearances) allow(s int, a Away, r float64) bool {
	return float64(c.road[s]) > a.Road+r && float64(c.water[s]) > a.Water+r && float64(c.camp[s]) > a.Camp+r &&
		float64(c.town[s]) > a.Town+r && float64(c.spot[s]) > a.Waypoint+r
}

func (b *builder) clearances() clearances {
	band := 1.0
	for _, name := range slices.Sorted(maps.Keys(b.p.Biomes)) {
		for _, sc := range b.p.Biomes[name].Scatter {
			a := sc.Away
			band = max(band, max(a.Road, a.Water, a.Camp, a.Town, a.Waypoint)+b.k.Models[sc.Model].reach()+1)
		}
	}
	far := func() []float32 {
		v := make([]float32, len(b.h))
		for i := range v {
			v[i] = float32(math.Inf(1))
		}
		return v
	}
	c := clearances{road: far(), water: far(), camp: far(), town: far(), spot: far()}
	keep := func(into []float32, s int, d float64) { into[s] = min(into[s], float32(max(d, 0))) }
	for _, r := range b.roads {
		b.f.near(b.g, r.Path, false, float64(r.Width/2)+band)
		for _, s := range b.f.touched {
			keep(c.road, int(s), math.Sqrt(b.f.d2[s])-float64(r.Width/2))
		}
	}
	for _, r := range b.rivers {
		b.f.near(b.g, r.Path, false, float64(r.Width/2)+band)
		for _, s := range b.f.touched {
			keep(c.water, int(s), math.Sqrt(b.f.d2[s])-float64(r.Width/2))
		}
	}
	areas := func(into []float32, poly [][2]float64) {
		b.f.near(b.g, poly, true, band)
		for _, s := range b.f.touched {
			keep(into, int(s), math.Sqrt(b.f.d2[s]))
		}
		b.g.fill(poly, func(s int) { keep(into, s, 0) })
	}
	for _, f := range b.p.Features {
		if f.Lake != nil {
			areas(c.water, f.Lake.Area)
		}
	}
	for _, a := range b.p.Areas {
		if a.Kind == Town {
			areas(c.town, a.Area)
		}
	}
	circle := func(into []float32, at [2]float64, radius float64) {
		reach := radius + band
		ix0, ix1 := span(at[0]-reach, at[0]+reach, b.g.cell, b.g.nx)
		iz0, iz1 := span(at[1]-reach, at[1]+reach, b.g.cell, b.g.nz)
		for iz := iz0; iz <= iz1; iz++ {
			for ix := ix0; ix <= ix1; ix++ {
				dx, dz := b.g.x(ix)-at[0], b.g.x(iz)-at[1]
				if d := math.Sqrt(float64(dx*dx) + float64(dz*dz)); d <= reach {
					keep(into, b.g.index(ix, iz), d-radius)
				}
			}
		}
	}
	for _, cp := range b.p.Camps {
		circle(c.camp, cp.At, cp.Radius)
	}
	for _, s := range b.p.spots() {
		circle(c.spot, s.at, s.radius)
	}
	return c
}
