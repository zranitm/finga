package maps3d

import (
	"crypto/sha256"
	"fmt"
	"math"
	"testing"
)

func TestQuarterTurnsAreExact(t *testing.T) {
	for _, tt := range []struct{ deg, sin, cos float64 }{
		{0, 0, 1}, {90, 1, 0}, {180, 0, -1}, {270, -1, 0}, {360, 0, 1}, {-90, -1, 0}, {450, 1, 0},
	} {
		s, c := sincos(tt.deg)
		if s != tt.sin || c != tt.cos || math.Signbit(s) && s == 0 || math.Signbit(c) && c == 0 {
			t.Errorf("sincos(%g) = %v, %v; want %v, %v with no -0", tt.deg, s, c, tt.sin, tt.cos)
		}
	}
}

// sweep is every 1/40 of a degree round the circle.
func sweep(f func(deg float64)) {
	for i := range 360 * 40 {
		f(float64(i) / 40)
	}
}

func TestSincosIsCloseToMath(t *testing.T) {
	sweep(func(deg float64) {
		s, c := sincos(deg)
		ms, mc := math.Sincos(deg * math.Pi / 180)
		if math.Abs(s-ms) > 2e-15 || math.Abs(c-mc) > 2e-15 {
			t.Fatalf("sincos(%g) = %v, %v; math gives %v, %v", deg, s, c, ms, mc)
		}
	})
}

// sincosBits is the hash of the sweep's bits, which every Go build must give, amd64 and arm64 alike: math.Sincos gives 314 of these
// 14,400 angles differently on the two.
const sincosBits = "be4ff528f9607fbd89af75a0ea194f204bf9301a7319f50c94b181a8892706a6"

func TestSincosBits(t *testing.T) {
	h := sha256.New()
	sweep(func(deg float64) {
		s, c := sincos(deg)
		fmt.Fprintf(h, "%x %x\n", math.Float64bits(s), math.Float64bits(c))
	})
	if got := fmt.Sprintf("%x", h.Sum(nil)); got != sincosBits {
		t.Errorf("the sweep's bits hash to %s, want %s", got, sincosBits)
	}
}
