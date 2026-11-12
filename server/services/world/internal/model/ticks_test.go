package model_test

import (
	"fmt"
	"testing"
	"time"

	"finga/services/world/internal/model"
)

// A tick's interval is cut to whole nanoseconds, so a duration of whole ticks must still count as that many, at every rate.
func TestTicksIn(t *testing.T) {
	tests := []struct {
		hz   int
		d    time.Duration
		want uint64
	}{
		{20, 600 * time.Millisecond, 12},
		{30, 600 * time.Millisecond, 18},
		{60, time.Second, 60},
		{60, time.Minute, 3600},
		{12, time.Second, 12},
		{15, 600 * time.Millisecond, 9},
		{20, 610 * time.Millisecond, 13}, // part of a tick counts as one
		{30, 610 * time.Millisecond, 19},
		{20, time.Millisecond, 1},
		{20, 0, 1}, // at least one
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v at %d Hz", tt.d, tt.hz), func(t *testing.T) {
			if got := model.TicksIn(tt.d, time.Second/time.Duration(tt.hz)); got != tt.want {
				t.Fatalf("got %d ticks, want %d", got, tt.want)
			}
		})
	}
}

// Attack speed shortens a class's attack before it is counted in whole ticks, rounded up; the vectors in testdata/actions pin the rest.
func TestAttackLengthTicks(t *testing.T) {
	tick := 50 * time.Millisecond
	tests := []struct {
		length time.Duration
		speed  float64
		want   uint64
	}{
		{600 * time.Millisecond, 0, 12},
		{600 * time.Millisecond, 0.016129032258064516, 12}, // a new warrior: 590 ms
		{600 * time.Millisecond, 0.2, 10},                  // exactly 500 ms
		{600 * time.Millisecond, 0.5, 8},                   // exactly 400 ms
		{800 * time.Millisecond, 0.5, 11},                  // 533 ms
	}
	for _, tt := range tests {
		if got := model.AttackLengthTicks(tt.length, tt.speed, tick); got != tt.want {
			t.Errorf("%v at speed %v: %d ticks, want %d", tt.length, tt.speed, got, tt.want)
		}
	}
}

// Cooldown speed cuts a skill's cooldown before it is counted in whole ticks, rounded up.
func TestSkillCooldownTicks(t *testing.T) {
	tick := 50 * time.Millisecond
	tests := []struct {
		cooldown time.Duration
		speed    float64
		want     uint64
	}{
		{2 * time.Second, 0, 40},
		{2 * time.Second, 0.25, 30},        // exactly 1.5 s
		{2 * time.Second, 0.2, 32},         // exactly 1.6 s
		{2 * time.Second, 0.21, 32},        // 1.58 s
		{2500 * time.Millisecond, 0.4, 30}, // the limit: exactly 1.5 s
	}
	for _, tt := range tests {
		if got := model.SkillCooldownTicks(tt.cooldown, tt.speed, tick); got != tt.want {
			t.Errorf("%v at speed %v: %d ticks, want %d", tt.cooldown, tt.speed, got, tt.want)
		}
	}
}
