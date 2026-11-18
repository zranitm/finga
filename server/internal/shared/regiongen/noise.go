package regiongen

import "math"

// noise is OpenSimplex 2D noise, ported from github.com/ojrac/opensimplex-go v1.0.2 (commit 67ecc326, Unlicense) with every product rounded on its own: on arm64 Go fuses the library's multiply-adds, and half its values differ from amd64's in their last bits.
type noise struct{ perm [256]int32 }

const (
	stretch2 = -0.211324865405187 // (1/√3 − 1)/2
	squish2  = 0.366025403784439  // (√3 − 1)/2
	norm2    = 47
)

// The gradients: directions to an octagon's corners.
var gradients2 = [...]float64{5, 2, 2, 5, -5, 2, -2, 5, 5, -2, 2, -5, -5, -2, -2, -5}

// newNoise is the library's New(seed): a permutation shuffled by a 64-bit LCG.
func newNoise(seed int64) *noise {
	var n noise
	var source [256]int32
	for i := range source {
		source[i] = int32(i)
	}
	for range 3 {
		seed = seed*6364136223846793005 + 1442695040888963407
	}
	for i := int32(255); i >= 0; i-- {
		seed = seed*6364136223846793005 + 1442695040888963407
		r := int32((seed + 31) % int64(i+1))
		if r < 0 {
			r += i + 1
		}
		n.perm[i] = source[r]
		source[r] = source[i]
	}
	return &n
}

// at is the library's Eval2(x, y), about ±0.87.
func (n *noise) at(x, y float64) float64 {
	offset := float64((x + y) * stretch2)
	xs, ys := x+offset, y+offset
	xsb, ysb := int32(math.Floor(xs)), int32(math.Floor(ys))
	squish := float64(float64(xsb+ysb) * squish2)
	xb, yb := float64(xsb)+squish, float64(ysb)+squish
	xins, yins := xs-float64(xsb), ys-float64(ysb)
	inSum := xins + yins
	dx0, dy0 := x-xb, y-yb

	value := float64(0)
	value = n.add(value, xsb+1, ysb+0, dx0-1-squish2, dy0-0-squish2)
	value = n.add(value, xsb+0, ysb+1, dx0-0-squish2, dy0-1-squish2)

	var dxExt, dyExt float64
	var xsvExt, ysvExt int32
	if inSum <= 1 { // in the triangle at (0, 0)
		zins := 1 - inSum
		switch {
		case (zins > xins || zins > yins) && xins > yins:
			xsvExt, ysvExt, dxExt, dyExt = xsb+1, ysb-1, dx0-1, dy0+1
		case zins > xins || zins > yins:
			xsvExt, ysvExt, dxExt, dyExt = xsb-1, ysb+1, dx0+1, dy0-1
		default:
			xsvExt, ysvExt, dxExt, dyExt = xsb+1, ysb+1, dx0-1-2*squish2, dy0-1-2*squish2
		}
	} else { // in the triangle at (1, 1)
		zins := 2 - inSum
		switch {
		case (zins < xins || zins < yins) && xins > yins:
			xsvExt, ysvExt, dxExt, dyExt = xsb+2, ysb+0, dx0-2-2*squish2, dy0+0-2*squish2
		case zins < xins || zins < yins:
			xsvExt, ysvExt, dxExt, dyExt = xsb+0, ysb+2, dx0+0-2*squish2, dy0-2-2*squish2
		default:
			xsvExt, ysvExt, dxExt, dyExt = xsb, ysb, dx0, dy0
		}
		xsb, ysb = xsb+1, ysb+1
		dx0, dy0 = dx0-1-2*squish2, dy0-1-2*squish2
	}
	value = n.add(value, xsb, ysb, dx0, dy0)
	value = n.add(value, xsvExt, ysvExt, dxExt, dyExt)
	return value / norm2
}

// add is value plus the lattice point (xsb, ysb)'s share at (dx, dy) from it, when it reaches that far.
func (n *noise) add(value float64, xsb, ysb int32, dx, dy float64) float64 {
	attn := 2 - float64(dx*dx) - float64(dy*dy)
	if attn > 0 {
		attn = float64(attn * attn)
		g := n.perm[(n.perm[xsb&0xFF]+ysb)&0xFF] & 0x0E
		value += float64(float64(attn*attn) * (float64(gradients2[g]*dx) + float64(gradients2[g+1]*dy)))
	}
	return value
}
