package sim3d

import (
	"math"
	"testing"
)

// A cell is steep along an axis when the rise across either of its edges along that axis exceeds MaxSlope × Cell, and only then.
func TestSteepMarks(t *testing.T) {
	cases := []struct {
		name           string
		heights        []float64 // 2 × 2 samples, cell 0.5: near row (z = 0), then far row
		steepX, steepZ bool
	}{
		{"a slope along x", []float64{0, 0.4, 0, 0.4}, true, false},
		{"a slope along z", []float64{0, 0, 0.4, 0.4}, false, true},
		{"one x edge over the limit", []float64{0, 0.4, 0.2, 0.4}, true, false},
		{"a rise of exactly max_slope × cell", []float64{0, 0.35, 0, 0.35}, false, false},
		{"a fall: the mark holds either way, and the move's way decides uphill", []float64{0.4, 0, 0.4, 0}, true, false},
		{"one raised corner: an x edge and a z edge", []float64{0, 0, 0, 0.4}, true, true},
	}
	for _, c := range cases {
		ter := NewTerrain(0.5, 2, 2, c.heights, 0.7)
		if ter.steepX[0] != c.steepX || ter.steepZ[0] != c.steepZ {
			t.Errorf("%s: steep along x %v, z %v; want %v, %v", c.name, ter.steepX[0], ter.steepZ[0], c.steepX, c.steepZ)
		}
	}
}

func TestHeightIsBilinear(t *testing.T) {
	ter := NewTerrain(2, 2, 2, []float64{0, 2, 4, 6}, 0.7)
	for _, c := range []struct{ x, z, want float64 }{{0, 0, 0}, {2, 0, 2}, {0, 2, 4}, {1, 1, 3}, {2, 2, 6}, {0.5, 1.5, 3.5}} {
		if got := ter.height(c.x, c.z); got != c.want {
			t.Errorf("height(%v, %v) = %v, want %v", c.x, c.z, got, c.want)
		}
	}
}

// At a -0 coordinate the cell fraction is -0, which a -0 sample carries through to the height; web/src/sim3d's clamp must keep it too.
func TestNegativeZeroKeepsItsSign(t *testing.T) {
	negZero := math.Copysign(0, -1)
	ter := NewTerrain(1, 2, 2, []float64{negZero, 1, negZero, 1}, 0.7)
	if got := ter.height(negZero, negZero); got != 0 || !math.Signbit(got) {
		t.Errorf("height(-0, -0) = %v with sign bit %v, want -0", got, math.Signbit(got))
	}
}
