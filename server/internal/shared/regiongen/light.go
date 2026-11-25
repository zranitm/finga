package regiongen

import "math"

// lightGroups is how many flicker groups the light map holds, a byte each per texel.
const lightGroups = 2

// lightMap is light.bin's light (underground-and-underwater.md section 4): 1 m texels over the region, row by row from the south-west, each the light from each flicker group's torches, 0 to 255. A torch's light at a texel d from it, within its reach r, is (1 − d²/r²)², where it sees the texel's ground: that ground below the flame, and no ground on the straight line between them, stepped every half cell, rising above it. So light stops at walls and rock tops stay dark.
func (r *Region) lightMap() []byte {
	t := r.Geometry.Terrain
	nx, nz := int(r.Plan.Size[0]), int(r.Plan.Size[1])
	sum := make([]float64, lightGroups*nx*nz)
	step := float64(t.Cell / 2)
	for _, f := range r.Flames {
		r2 := float64(f.Reach * f.Reach)
		x0, x1 := max(int(math.Floor(f.At[0]-f.Reach)), 0), min(int(math.Ceil(f.At[0]+f.Reach)), nx-1)
		z0, z1 := max(int(math.Floor(f.At[1]-f.Reach)), 0), min(int(math.Ceil(f.At[1]+f.Reach)), nz-1)
		for iz := z0; iz <= z1; iz++ {
			for ix := x0; ix <= x1; ix++ {
				x, z := float64(ix)+0.5, float64(iz)+0.5
				dx, dz := x-f.At[0], z-f.At[1]
				d2 := float64(dx*dx) + float64(dz*dz)
				if d2 >= r2 {
					continue
				}
				ground := t.Height(x, z)
				if ground >= f.Y || !sees(t.Height, f, x, z, ground, math.Sqrt(d2), step) {
					continue
				}
				w := 1 - d2/r2
				sum[lightGroups*(iz*nx+ix)+f.Group] += float64(w * w)
			}
		}
	}
	out := make([]byte, len(sum))
	for i, v := range sum {
		out[i] = byte(math.Floor(float64(min(v, 1) * 255)))
	}
	return out
}

// sees is whether flame f sees the ground at (x, z), d from it: no ground at the steps every step metres between them rises above the line from the flame down to that ground.
func sees(height func(x, z float64) float64, f Flame, x, z, ground, d, step float64) bool {
	n := int(math.Ceil(d / step))
	for k := 1; k < n; k++ {
		s := float64(k) / float64(n)
		px, pz := lerp(f.At[0], x, s), lerp(f.At[1], z, s)
		if height(px, pz) > lerp(f.Y, ground, s) {
			return false
		}
	}
	return true
}

// light is light.bin: "FRLT", the format, its texels along x and z and the bytes in each, then lightMap's texels; none for a region without torches.
func (r *Region) light() []byte {
	var w writer
	w.header("FRLT", int(r.Plan.Size[0]), int(r.Plan.Size[1]), lightGroups)
	w.b = append(w.b, r.lightMap()...)
	return w.b
}
