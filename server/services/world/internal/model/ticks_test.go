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
