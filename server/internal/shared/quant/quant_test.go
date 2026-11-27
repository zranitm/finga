package quant_test

import (
	"encoding/json"
	"os"
	"testing"

	"finga/internal/shared/quant"
)

// The vectors are the client's too (web/src/protocol/expand.test.ts): both round a value to the same whole number, so the steps run from the same baseline.
func TestRoundingMatchesTheSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("../../../testdata/quant.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Position []struct {
			Metres float64
			CM     int32
		}
		Speed []struct {
			MetresPerSecond float64
			MM              int32
		}
		Yaw []struct {
			Radians float64
			Milli   int32
		}
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	for _, c := range v.Position {
		if got := quant.Position(c.Metres); got != c.CM {
			t.Errorf("position %v: %d, want %d", c.Metres, got, c.CM)
		}
	}
	for _, c := range v.Speed {
		if got := quant.Speed(c.MetresPerSecond); got != c.MM {
			t.Errorf("speed %v: %d, want %d", c.MetresPerSecond, got, c.MM)
		}
	}
	for _, c := range v.Yaw {
		if got := quant.Yaw(float32(c.Radians)); got != c.Milli {
			t.Errorf("yaw %v: %d, want %d", c.Radians, got, c.Milli)
		}
	}
}
