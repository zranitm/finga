package model

import (
	"cmp"
	"math"
	"slices"

	"finga/internal/gamedata"
)

// Body is an upright cylinder a shape may reach: its feet centre, radius and height, in metres with y up.
type Body struct {
	X, Y, Z        float64
	Radius, Height float64
}

// Aim is where an action starts and the way it goes: the attacker's feet centre and its yaw, radians from +z toward +x.
type Aim struct {
	X, Y, Z float64
	Yaw     float64
}

// Reached are the bodies a shape reaches from aim, nearest first, at most most of them (3d-world.md section 6):
//   - arc: those whose circle meets the sector of Radius and Angle about the aim, and whose height meets 0 to Height above the feet, by distance to their centre;
//   - shot: those whose circle meets the lane Width wide along the aim up to Range, by distance along it;
//   - area: those whose circle meets the circle of Radius about the attacker, by distance to their nearest point.
func Reached(aim Aim, s gamedata.Shape, bodies []Body, most int) []int {
	type found struct {
		index int
		dist  float64
	}
	sin, cos := math.Sincos(aim.Yaw)
	halfSin, halfCos := math.Sincos(s.Angle * math.Pi / 360)
	var hits []found
	for i, b := range bodies {
		px, pz := b.X-aim.X, b.Z-aim.Z
		along, side := px*sin+pz*cos, math.Abs(px*cos-pz*sin)
		d := math.Hypot(px, pz)
		switch s.Kind {
		case gamedata.ArcShape:
			if b.Y <= aim.Y+s.Height && b.Y+b.Height >= aim.Y && arcMeets(d, along, side, s.Radius, halfSin, halfCos, b.Radius) {
				hits = append(hits, found{i, d})
			}
		case gamedata.ShotShape:
			if math.Hypot(along-clamp(along, 0, s.Range), max(0, side-s.Width/2)) <= b.Radius {
				hits = append(hits, found{i, max(0, along-b.Radius)})
			}
		case gamedata.AreaShape:
			if d <= s.Radius+b.Radius {
				hits = append(hits, found{i, max(0, d-b.Radius)})
			}
		}
	}
	slices.SortStableFunc(hits, func(a, b found) int { return cmp.Compare(a.dist, b.dist) })
	out := make([]int, 0, min(most, len(hits)))
	for _, h := range hits[:min(most, len(hits))] {
		out = append(out, h.index)
	}
	return out
}

// arcMeets reports whether a circle of radius r, its centre d from the apex, along the aim and side of it, meets the sector of radius and half angle.
func arcMeets(d, along, side, radius, halfSin, halfCos, r float64) bool {
	if along >= d*halfCos { // within the angle: the sector's own radius
		return d <= radius+r
	}
	t := clamp(along*halfCos+side*halfSin, 0, radius) // the nearest point on the nearer edge
	return math.Hypot(along-t*halfCos, side-t*halfSin) <= r
}

func clamp(v, lo, hi float64) float64 { return max(lo, min(v, hi)) }

// Gap is how far the point (x, y, z) lies from b's nearest point, 0 inside it.
func Gap(x, y, z float64, b Body) float64 {
	across := max(0, math.Hypot(x-b.X, z-b.Z)-b.Radius)
	up := max(0, b.Y-y, y-(b.Y+b.Height))
	return math.Hypot(across, up)
}
