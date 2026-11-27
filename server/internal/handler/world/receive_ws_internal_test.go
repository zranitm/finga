package world

import (
	"errors"
	"finga/internal/shared/failure"
	"finga/internal/shared/kernel"
	"math"
	"testing"
)

// An attack's or a skill's aim passes when finite, and is a malformed body when NaN or infinite.
func TestAnAimMustBeFinite(t *testing.T) {
	for _, tt := range []struct {
		name string
		yaw  float32
		err  error
	}{
		{"finite", -2.5, nil},
		{"zero", 0, nil},
		{"NaN", float32(math.NaN()), failure.ErrBadAim},
		{"+Inf", float32(math.Inf(1)), failure.ErrBadAim},
		{"-Inf", float32(math.Inf(-1)), failure.ErrBadAim},
	} {
		t.Run(tt.name, func(t *testing.T) {
			aim, err := aimOf(tt.yaw)
			if !errors.Is(err, tt.err) || (err == nil && aim != tt.yaw) {
				t.Fatalf("aimOf(%v) = %v, %v; want %v", tt.yaw, aim, err, tt.err)
			}
		})
	}
}

// A class the game data lacks is the world's own fault, so a world command fails internal, though character's Create refuses with the same rule; every other world rule keeps its own failure.
func TestAWorldCommandMissingAClassIsInternal(t *testing.T) {
	if got := failure.PublicOf(worldErr(kernel.ErrClassUnavailable)); got != failure.Internal {
		t.Fatalf("the world's class_unavailable is %s", got.Name)
	}
	if got := failure.PublicOf(kernel.ErrClassUnavailable); got != failure.ClassUnavailable {
		t.Fatalf("character's class_unavailable is %s", got.Name)
	}
	if got := failure.PublicOf(worldErr(kernel.ErrNotEnoughEnergy)); got != failure.NotEnoughEnergy {
		t.Fatalf("another world rule is %s", got.Name)
	}
}
