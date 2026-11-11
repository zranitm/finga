package gamedata_test

import (
	"math"
	"testing"

	"finga/internal/gamedata"
)

func TestHitChance(t *testing.T) {
	curve := gamedata.HitCurve{Top: 0.97, K: 0.12, Power: 2, Min: 0.20, Max: 0.99}
	for _, c := range []struct {
		name          string
		accuracy, eva float64
		want          float64
	}{
		{"no evasion is the top", 30, 0, 0.97},
		{"evasion as high as accuracy", 30, 30, 0.97 / 1.12},
		{"twice the accuracy", 30, 60, 0.97 / 1.48},
		{"far past it, held at the least", 30, 300, 0.20},
		{"no accuracy against evasion, held at the least", 0, 10, 0.20},
		{"neither", 0, 0, 0.97},
	} {
		if got := curve.Chance(c.accuracy, c.eva); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	if got := (gamedata.HitCurve{Top: 1, K: 0.12, Power: 2, Min: 0.2, Max: 0.99}).Chance(30, 0); got != 0.99 {
		t.Errorf("a top past the most: %v, want 0.99", got)
	}
	// Evasion takes less off with each step once well past accuracy.
	step := func(eva float64) float64 { return curve.Chance(30, eva) - curve.Chance(30, eva+10) }
	if !(step(60) > step(80) && step(80) > step(100)) {
		t.Errorf("steps past twice the accuracy: %v, %v, %v; want each smaller", step(60), step(80), step(100))
	}
}
