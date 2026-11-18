package regiongen

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"testing"
)

// The port gives the library's own bits as amd64 computes them, on every CPU: each grid's hash was taken from github.com/ojrac/opensimplex-go v1.0.2 built for amd64, where Go fuses nothing. On arm64 the library itself gives other hashes (d8b2726d…, 4b7cfa80…, fb85358f…).
func TestTheNoiseIsTheLibrarysAsAmd64ComputesIt(t *testing.T) {
	for _, c := range []struct {
		seed int64
		n    int
		at   func(i int) float64
		want string
	}{
		{11, 257, func(i int) float64 { return float64(i) / 55 }, "8a00151af8d03834406eda5080523a6f37540e335f7d8ec406a71c5c8233b0bb"},
		{11, 257, func(i int) float64 { return float64(i) / 23.7 }, "bab17743b4394a5195e56e2b5a20660b66b870e2aa7656e44b74c7b6fa6bb0af"},
		{-7, 257, func(i int) float64 { return (float64(i) - 128) / 9.3 }, "4d40d47a6e5d45f9023185a80b458f0fbde62e3143fc90e2b7c4489b067eb0d9"},
	} {
		n := newNoise(c.seed)
		h := sha256.New()
		var b [8]byte
		for z := range c.n {
			for x := range c.n {
				binary.LittleEndian.PutUint64(b[:], math.Float64bits(n.at(c.at(x), c.at(z))))
				h.Write(b[:])
			}
		}
		if got := fmt.Sprintf("%x", h.Sum(nil)); got != c.want {
			t.Errorf("seed %d over %d², hash %s; want the library's %s", c.seed, c.n, got, c.want)
		}
	}
	if got, got2 := newNoise(11).at(1.25, -3.5), newNoise(-7).at(-100.125, 42.75); got != 0.33533847807820155 || got2 != 0.03956341680488316 {
		t.Errorf("noise %v and %v; want the library's 0.33533847807820155 and 0.03956341680488316", got, got2)
	}
}
