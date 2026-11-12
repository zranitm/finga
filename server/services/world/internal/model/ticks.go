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

// AttackLengthTicks is a basic attack's length sped up by attack speed, in ticks of interval: ⌈length ÷ (1 + speed) ÷ interval⌉, the sped-up length first cut to whole nanoseconds.
func AttackLengthTicks(length time.Duration, speed float64, interval time.Duration) uint64 {
	return TicksIn(time.Duration(math.Round(float64(length)/(1+speed))), interval)
}

// SkillLengthTicks is a skill's length in ticks of interval; attack speed doesn't change it.
func SkillLengthTicks(length, interval time.Duration) uint64 {
	return TicksIn(length, interval)
}

// ActionPoints are an action's hit and cancel point in ticks from its start, ⌊length × share⌋ each, the cancel point held to at least the hit and one tick.
func ActionPoints(length uint64, hit, cancel float64) (hitAt, cancelAt uint64) {
	hitAt = ticksOf(length, hit)
	return hitAt, max(ticksOf(length, cancel), hitAt, 1)
}

// ticksOf is ⌊ticks × share⌋; a product within rounding of a whole number counts as it.
func ticksOf(ticks uint64, share float64) uint64 {
	x := float64(ticks) * share
	if whole := math.Round(x); math.Abs(x-whole) <= whole*wholeTicks {
		return uint64(whole)
	}
	return uint64(math.Floor(x))
}

// SkillCooldownTicks is a skill's cooldown cut by cooldown speed, in ticks of interval: ⌈cooldown × (1 − speed) ÷ interval⌉, the cut cooldown first cut to whole nanoseconds.
func SkillCooldownTicks(cooldown time.Duration, speed float64, interval time.Duration) uint64 {
	return TicksIn(time.Duration(math.Round(float64(cooldown)*(1-speed))), interval)
}
