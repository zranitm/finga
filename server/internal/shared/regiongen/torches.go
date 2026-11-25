package regiongen

import (
	"math"

	"finga/internal/shared/mapfile"
)

// torchAway is how far a torch keeps from gates, waypoints and other torches.
const torchAway = 3

// Flame is a torch's light as the bake gives it: its flame's spot, height and flicker group.
type Flame struct {
	At    [2]float64 `json:"at"`
	Y     float64    `json:"y"`
	Group int        `json:"group"`
	Reach float64    `json:"-"` // how far its light falls, which the light map holds
}

// facings are the ways a prop turned 0°, 22.5°, … 337.5° faces, x east and z north: across mapfile.Axis's width. A torch takes the nearest, as the generator takes no angle from a direction.
var facings = func() (out [16][2]float64) {
	for k := range out {
		ux, uz := mapfile.Axis(float64(k) * 22.5)
		out[k] = [2]float64{-uz, ux}
	}
	return out
}()

// yawOf is the yaw of the facing nearest the way (dx, dz).
func yawOf(dx, dz float64) float64 {
	best, most := 0, math.Inf(-1)
	for k, d := range facings {
		if dot := float64(dx*d[0]) + float64(dz*d[1]); dot > most {
			best, most = k, dot
		}
	}
	return float64(best) * 22.5
}

// torches hangs the plan's torches on each hall's and tunnel's walls, in plan order, one every Spacing, facing in (underground-and-underwater.md section 4).
func (b *builder) torches(placed *solidsNear) {
	t := b.p.Torches
	if t == nil {
		return
	}
	for _, f := range b.p.Features {
		switch {
		case f.Hall != nil:
			o := float64(orientation(f.Hall.Area)) // anticlockwise corners hold the inside left of each edge
			walkPath(f.Hall.Area, true, t.Spacing/2, t.Spacing, func(at, dir [2]float64) {
				n := [2]float64{float64(o * dir[1]), float64(-o * dir[0])}                                             // outward
				from := [2]float64{at[0] - float64(n[0]*(b.p.Rock.Rough+1)), at[1] - float64(n[1]*(b.p.Rock.Rough+1))} // inside, as halls are at least twice this wide
				b.hang(from, n, float64(2*(b.p.Rock.Rough+1))+b.p.wallReach(), t.Model, placed)
			})
		case f.Tunnel != nil:
			half, path := float64(f.Tunnel.Width/2), smooth(f.Tunnel.Path)
			for side, first := range []float64{0, t.Spacing / 2} { // the left wall, then the right half a spacing on, so neighbours on a wall flicker apart
				left := float64(1 - 2*side)
				walkPath(path, false, first, t.Spacing, func(at, dir [2]float64) {
					b.hang(at, [2]float64{-left * dir[1], left * dir[0]}, half+b.p.wallReach(), t.Model, placed)
				})
			}
		}
	}
}

// hang walks from from along unit n, half a cell a step up to far, to the first rock, and hangs model facing back a cell and a half short of it, on the floor below the wall's slope, if that's clear.
func (b *builder) hang(from, n [2]float64, far float64, model string, placed *solidsNear) {
	step := float64(b.g.cell / 2)
	for d := 0.0; d <= far; d += step {
		at := [2]float64{from[0] + float64(n[0]*d), from[1] + float64(n[1]*d)}
		if !b.rocky[b.g.nearest(at[0], at[1])] {
			continue
		}
		if d == 0 { // from is rock already
			return
		}
		back := max(d-float64(3*step), 0)
		x, z := f32(from[0]+float64(n[0]*back)), f32(from[1]+float64(n[1]*back))
		if b.torchClear(x, z) {
			b.place(model, x, z, yawOf(-n[0], -n[1]), placed)
		}
		return
	}
}

// torchClear is whether a torch at (x, z) keeps torchAway from every gate, waypoint and torch already hung, and stands outside every camp's and boss's circle.
func (b *builder) torchClear(x, z float64) bool {
	far := func(at [2]float64, r float64) bool {
		dx, dz := x-at[0], z-at[1]
		return float64(dx*dx)+float64(dz*dz) > float64(r*r)
	}
	for _, g := range b.p.Gates {
		if !far(g.At, torchAway) {
			return false
		}
	}
	for _, w := range b.p.Waypoints {
		if !far(w.At, torchAway) {
			return false
		}
	}
	for _, c := range b.p.Camps {
		if !far(c.At, c.Radius) {
			return false
		}
	}
	for _, a := range b.p.Areas {
		if a.Boss != nil && !far(a.Boss.At, a.Boss.Radius) {
			return false
		}
	}
	for _, pr := range b.r.Props {
		if b.k.Models[b.r.Models[pr.Model]].Light != nil && !far([2]float64{pr.X, pr.Z}, torchAway) {
			return false
		}
	}
	return true
}

// walkPath calls at for the points first, first+every, … along path, closed back to its start when closed, each with its segment's unit direction.
func walkPath(path [][2]float64, closed bool, first, every float64, at func(p, dir [2]float64)) {
	n := len(path) - 1
	if closed {
		n = len(path)
	}
	next, done := first, 0.0
	for i := range n {
		a, c := path[i], path[(i+1)%len(path)]
		dx, dz := c[0]-a[0], c[1]-a[1]
		l := math.Sqrt(float64(dx*dx) + float64(dz*dz))
		if l == 0 {
			continue
		}
		dir := [2]float64{dx / l, dz / l}
		for ; next <= done+l; next += every {
			t := (next - done) / l
			at([2]float64{lerp(a[0], c[0], t), lerp(a[1], c[1], t)}, dir)
		}
		done += l
	}
}

// orientation is 1 for a polygon whose corners turn anticlockwise, x east and z north, and -1 for clockwise.
func orientation(poly [][2]float64) int {
	area := 0.0
	for i, a := range poly {
		c := poly[(i+1)%len(poly)]
		area += float64(a[0]*c[1]) - float64(c[0]*a[1])
	}
	if area > 0 {
		return 1
	}
	return -1
}

// flames are the region's torches' lights, in the props' order: every prop of a model with a light, set pieces first.
func (b *builder) flames() {
	t := b.r.Geometry.Terrain
	for _, pr := range b.r.Props {
		l := b.k.Models[b.r.Models[pr.Model]].Light
		if l == nil {
			continue
		}
		b.r.Flames = append(b.r.Flames, Flame{At: [2]float64{pr.X, pr.Z}, Y: f32(t.Height(pr.X, pr.Z) + l.Height), Group: len(b.r.Flames) % 2, Reach: l.Reach})
	}
}
