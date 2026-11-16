package config_test

import (
	"testing"
	"time"

	"finga/modules/world/config"
)

func TestTheTickIntervalFollowsFromTheRate(t *testing.T) {
	if got := (config.Config{TickRateHz: 20}).TickInterval(); got != 50*time.Millisecond {
		t.Fatalf("tick interval %v", got)
	}
}
