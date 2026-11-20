package load

import (
	"crypto/sha256"
	"errors"
	"testing"

	"finga/internal/shared/sim"
)

// A generation that panics is an error for that load and every later load of the same plan, never an empty region.
func TestAGenerationThatPanicsIsAnErrorForEveryLoad(t *testing.T) {
	key := sha256.Sum256([]byte(t.Name()))
	_, _, err := shared(key, func() (sim.Geometry, string, error) { panic("a broken plan") })
	if err == nil || err.Error() != "generating it panicked: a broken plan" {
		t.Fatalf("first load: %v", err)
	}
	g, folder, again := shared(key, func() (sim.Geometry, string, error) { return sim.Geometry{SizeX: 1}, "abc", nil })
	if !errors.Is(again, err) || g.SizeX != 0 || folder != "" {
		t.Fatalf("a later load: %v, %v, %q", again, g, folder)
	}
}
