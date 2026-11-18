package regiongen

import (
	"image"
	"image/color"
	"math"
)

// previewPx is about how wide a preview is drawn; a small region is scaled up to it.
const previewPx = 1024

// The preview's marks.
var (
	propMark     = color.NRGBA{R: 30, G: 50, B: 20, A: 255}
	areaMark     = color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	campMark     = color.NRGBA{R: 220, G: 40, B: 40, A: 255}
	bossMark     = color.NRGBA{R: 255, G: 140, B: 0, A: 255}
	waypointMark = color.NRGBA{R: 0, G: 220, B: 230, A: 255}
	gateMark     = color.NRGBA{R: 230, G: 0, B: 230, A: 255}
	npcMark      = color.NRGBA{R: 255, G: 230, B: 0, A: 255}
	startMark    = color.NRGBA{R: 0, G: 200, B: 0, A: 255}
)

// Preview is the region from above to design it by: its image scaled up toward previewPx, each prop a dot, area outlines white, camps red, bosses orange, waypoints cyan, gates magenta, NPCs yellow and the start green.
func (r *Region) Preview() *image.NRGBA {
	base := r.Image()
	k := max(1, previewPx/max(base.Bounds().Dx(), base.Bounds().Dy()))
	w, h := base.Bounds().Dx()*k, base.Bounds().Dy()*k
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for by := range base.Bounds().Dy() {
		row := img.Pix[by*k*img.Stride : (by*k+1)*img.Stride]
		for x := range w {
			copy(row[4*x:4*x+4], base.Pix[by*base.Stride+4*(x/k):])
		}
		for dy := 1; dy < k; dy++ {
			copy(img.Pix[(by*k+dy)*img.Stride:], row)
		}
	}
	px := func(x, z float64) (int, int) { return int(x * float64(k)), h - 1 - int(z*float64(k)) }
	dot := func(x, z float64, c color.NRGBA, size int) {
		cx, cy := px(x, z)
		for dy := -size; dy <= size; dy++ {
			for dx := -size; dx <= size; dx++ {
				if image.Pt(cx+dx, cy+dy).In(img.Bounds()) {
					img.SetNRGBA(cx+dx, cy+dy, c)
				}
			}
		}
	}
	line := func(a, b [2]float64, c color.NRGBA) {
		steps := int(math.Ceil(math.Sqrt(float64((b[0]-a[0])*(b[0]-a[0]))+float64((b[1]-a[1])*(b[1]-a[1]))) * float64(2*k)))
		for i := range steps + 1 {
			p := lerp2(a, b, float64(i)/float64(max(steps, 1)))
			dot(p[0], p[1], c, 0)
		}
	}
	ring := func(at [2]float64, radius float64, c color.NRGBA) {
		// a polygon of 32 sides, its corners from a quarter's exact steps
		var pts [][2]float64
		for i := range 32 {
			ux, uz := unitCircle(i)
			pts = append(pts, [2]float64{at[0] + float64(radius*ux), at[1] + float64(radius*uz)})
		}
		for i := range pts {
			line(pts[i], pts[(i+1)%len(pts)], c)
		}
	}
	for _, p := range r.Props {
		dot(p.X, p.Z, propMark, k/4)
	}
	for _, a := range r.Plan.Areas {
		for i := range a.Area {
			line(a.Area[i], a.Area[(i+1)%len(a.Area)], areaMark)
		}
		if a.Boss != nil {
			ring(a.Boss.At, a.Boss.Radius, bossMark)
		}
	}
	for _, c := range r.Plan.Camps {
		ring(c.At, c.Radius, campMark)
	}
	for _, g := range r.Plan.Gates {
		ring(g.At, g.Radius, gateMark)
	}
	for _, wp := range r.Plan.Waypoints {
		dot(wp.At[0], wp.At[1], waypointMark, 2*k)
	}
	for _, n := range r.Plan.NPCs {
		dot(n.At[0], n.At[1], npcMark, k)
	}
	dot(r.Plan.Start.At[0], r.Plan.Start.At[1], startMark, 2*k)
	return img
}

// unitCircle is the i-th of 32 points round a unit circle, from a table so no sine is taken.
func unitCircle(i int) (float64, float64) {
	q := [...]float64{0, 0.19509032201612825, 0.3826834323650898, 0.5555702330196022, 0.7071067811865476, 0.8314696123025452, 0.9238795325112867, 0.9807852804032304, 1}
	j := i % 8
	switch i / 8 {
	case 0:
		return q[8-j], q[j]
	case 1:
		return -q[j], q[8-j]
	case 2:
		return -q[8-j], -q[j]
	default:
		return q[j], -q[8-j]
	}
}
