package world

import (
	"cmp"
	"math"
	"slices"

	"finga/internal/shared/gamedata"
	"finga/internal/shared/nav"
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

// Reached are the bodies a shape from aim reaches, nearest first, at most most (3d-world.md section 6); shots and areas test x and z only.
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
		along, side := float64(px*sin)+float64(pz*cos), math.Abs(float64(px*cos)-float64(pz*sin))
		d := nav.Length(px, pz)
		switch s.Kind {
		case gamedata.ArcShape:
			if b.Y <= aim.Y+s.Height && b.Y+b.Height >= aim.Y && arcMeets(d, along, side, s.Radius, halfSin, halfCos, b.Radius) {
				hits = append(hits, found{i, d})
			}
		case gamedata.ShotShape:
			if nav.Length(along-clamp(along, 0, s.Range), max(0, side-s.Width/2)) <= b.Radius {
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
	t := clamp(float64(along*halfCos)+float64(side*halfSin), 0, radius) // the nearest point on the nearer edge
	return nav.Length(along-float64(t*halfCos), side-float64(t*halfSin)) <= r
}

func clamp(v, lo, hi float64) float64 { return max(lo, min(v, hi)) }

// Gap is how far the point (x, y, z) lies from b's nearest point, 0 inside it.
func Gap(x, y, z float64, b Body) float64 {
	across := max(0, nav.Length(x-b.X, z-b.Z)-b.Radius)
	up := max(0, b.Y-y, y-(b.Y+b.Height))
	return nav.Length(across, up)
}
