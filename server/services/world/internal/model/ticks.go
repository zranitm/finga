package model

import "time"

// TicksIn is how many ticks of interval d lasts, rounded up and at least one.
func TicksIn(d, interval time.Duration) uint64 {
	return max(uint64((d+interval-1)/interval), 1)
}
