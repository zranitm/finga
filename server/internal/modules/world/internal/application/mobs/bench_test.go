package mobs_test

import (
	"math"
	"testing"

	"finga/internal/sim"
	"finga/modules/world/internal/application/mobs"
)

// Thirty mobs chasing one player running circles round a rock: one op is one tick.
func BenchmarkThirtyMobsChaseARunningPlayer(b *testing.B) {
	g := open(sim.Box{X: 20, Z: 20, HalfW: 1.5, HalfD: 1.5, UX: 1, Top: 3})
	a := newArena(b, meadow(b, g, spawn("wild_boar", 15, 12, 12, 4), spawn("green_slime", 15, 28, 28, 4)), &cycle{v: []float64{0.1, 0.7, 0.4, 0.9, 0.25}})
	p := player(20, 26)
	run := func(n uint64) {
		turn := 5 * tick.Seconds() * float64(n) / 6 // 5 m/s round a circle 6 m wide
		p.X, p.Z = 20+6*math.Sin(turn), 20+6*math.Cos(turn)
		a.Step(n, []mobs.Body{p})
	}
	for n := range uint64(200) { // the crowd gathers first
		run(n + 1)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := range uint64(b.N) {
		run(201 + n)
	}
}
