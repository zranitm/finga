package regiongen

import "math"

// lakeBank and lakeMitre are how the client's water.ts grows a lake's surface: each side lakeBank out, each corner lakeMitre banks out at most.
const (
	lakeBank  = 2
	lakeMitre = 3
)

// grown is lake area's ring as its surface is drawn: each corner moved out along the line between its sides' normals, lakeBank from both, lakeMitre banks at most.
func grown(area [][2]float64) [][2]float64 {
	n := len(area)
	twice := 0.0
	for i, a := range area {
		b := area[(i+1)%n]
		twice += float64(a[0]*b[1]) - float64(b[0]*a[1])
	}
	out := 1.0
	if twice < 0 {
		out = -1
	}
	normal := func(p, q [2]float64) [2]float64 {
		dx, dz := q[0]-p[0], q[1]-p[1]
		l := math.Sqrt(float64(dx*dx) + float64(dz*dz))
		if l == 0 {
			l = 1
		}
		return [2]float64{float64(out*dz) / l, float64(-out*dx) / l}
	}
	ring := make([][2]float64, n)
	for i, p := range area {
		n1, n2 := normal(area[(i+n-1)%n], p), normal(p, area[(i+1)%n])
		bx, bz := n1[0]+n2[0], n1[1]+n2[1]
		l := math.Sqrt(float64(bx*bx) + float64(bz*bz))
		if l == 0 {
			l = 1
		}
		cos := (float64(bx*n1[0]) + float64(bz*n1[1])) / l
		reach := min(lakeBank/max(cos, 1e-6), lakeMitre*lakeBank)
		ring[i] = [2]float64{p[0] + float64(bx/l*reach), p[1] + float64(bz/l*reach)}
	}
	return ring
}

// crosses is whether two of ring's sides touch or cross, apart from neighbours at their corner; sides along one line pass, as a fold there touches its neighbours too.
func crosses(ring [][2]float64) bool {
	n := len(ring)
	side := func(a, b, p [2]float64) float64 {
		return float64((b[0]-a[0])*(p[1]-a[1])) - float64((b[1]-a[1])*(p[0]-a[0]))
	}
	for i := range n {
		a, b := ring[i], ring[(i+1)%n]
		for j := i + 2; j < n; j++ {
			if i == 0 && j == n-1 {
				continue
			}
			c, d := ring[j], ring[(j+1)%n]
			s1, s2 := side(a, b, c), side(a, b, d)
			if s1 == 0 && s2 == 0 {
				continue
			}
			if s1*s2 <= 0 && side(c, d, a)*side(c, d, b) <= 0 {
				return true
			}
		}
	}
	return false
}
