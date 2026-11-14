package world_test

import (
	"log/slog"
	"strings"
	"testing"
	"time"

	"finga/internal/host"
	"finga/modules/world"
	"finga/modules/world/config"
	"finga/modules/world/internal/local"
	"finga/pkg/observability/metrics"
)

// built is what world.New answers for a tick of hz over the game's own data; the other modules are never called while it builds.
func built(t *testing.T, hz int) error {
	t.Helper()
	h, err := host.New(host.Options{Name: "finga", Settings: host.Settings{DataDir: "../../data"}, Logger: slog.New(slog.DiscardHandler), Registry: metrics.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	data, err := h.GameData()
	if err != nil {
		t.Fatal(err)
	}
	m, err := h.Module("world")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{TickRateHz: hz, CheckpointInterval: 30 * time.Second, FlushTimeout: time.Second, ItemCalls: config.ItemCalls{Workers: 1, BatchMax: 32}}
	_, err = world.New(t.Context(), world.Deps{Module: m, Config: cfg, Data: data, ID: "w",
		Character: struct{ local.CharacterModule }{}, Item: struct{ local.ItemModule }{}, Social: struct{ local.SocialModule }{}})
	return err
}

// A tick that splits a movement step is refused, as the client predicts in whole steps: 25 Hz holds 2.4 steps of 1/60 s, and 120 Hz half of one.
func TestATickThatSplitsAMovementStepIsRefused(t *testing.T) {
	for _, hz := range []int{25, 120} {
		if err := built(t, hz); err == nil || !strings.Contains(err.Error(), "tick_rate_hz") {
			t.Fatalf("%d Hz: %v", hz, err)
		}
	}
}

// One second holds whole movement steps, but it outlasts the Warrior's 900 ms attack.
func TestATickLongerThanAnActionIsRefused(t *testing.T) {
	if err := built(t, 1); err == nil || !strings.Contains(err.Error(), "tick_rate_hz") || !strings.Contains(err.Error(), "warrior") {
		t.Fatalf("1 Hz: %v", err)
	}
}

// The interval is cut to whole nanoseconds, so 30 Hz holds 1.99999998 steps of 1/60 s; the rate itself divides evenly.
func TestATickOfWholeStepsIsAcceptedAtAnyRate(t *testing.T) {
	for _, hz := range []int{10, 12, 15, 20, 30, 60} {
		if err := built(t, hz); err != nil {
			t.Fatalf("%d Hz: %v", hz, err)
		}
	}
}
