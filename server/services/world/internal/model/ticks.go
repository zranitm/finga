package model

import (
	"math"
	"time"
)

// wholeTicks is how near, relative to its count, a duration must be to whole ticks to count as them: the interval was cut to whole nanoseconds.
const wholeTicks = 1e-6

// TicksIn is how many ticks of interval d lasts, rounded up and at least one; a duration of whole ticks counts as exactly that many.
func TicksIn(d, interval time.Duration) uint64 {
	ticks := float64(d) / float64(interval)
	if whole := math.Round(ticks); math.Abs(ticks-whole) <= whole*wholeTicks {
		return max(uint64(whole), 1)
	}
	return max(uint64(math.Ceil(ticks)), 1)
}
