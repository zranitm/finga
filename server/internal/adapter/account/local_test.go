package account

import (
	"context"
	"errors"
	"finga/internal/shared/failure"
	"fmt"
	"testing"

	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// A module's failure reaches the use cases with its Public and the kind they act on, and keeps its cause.
func TestWrapNamesWhatTheUseCasesActOn(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind error // nil: none
		pub  failure.Public
	}{
		{"a final rule is its Public", fmt.Errorf("invite: %w", kernel.ErrPartyFull), nil, failure.PartyFull},
		{"the world not holding the stream", kernel.ErrNotSubscribed, failure.ErrNotSubscribed, failure.Internal},
		{"a module down for now", fmt.Errorf("%w: pool closed", transient.ErrUnavailable), failure.ErrUnavailable, failure.Unavailable},
		{"a world too busy to take a join", kernel.ErrMapBusy, failure.ErrUnavailable, failure.Unavailable},
		{"a world that is stopping", kernel.ErrWorldStopped, failure.ErrUnavailable, failure.Unavailable},
		{"a module's own failure", kernel.ErrInternal, nil, failure.Internal},
		{"a caller that gave up", context.Canceled, nil, failure.Internal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := wrap(tt.err)
			if got := failure.PublicOf(err); got != tt.pub {
				t.Fatalf("PublicOf = %+v, want %s", got, tt.pub.Name)
			}
			for _, kind := range []error{failure.ErrNotSubscribed, failure.ErrUnavailable} {
				if errors.Is(err, kind) != errors.Is(tt.kind, kind) {
					t.Fatalf("errors.Is(%v) = %v", kind, errors.Is(err, kind))
				}
			}
			if !errors.Is(err, tt.err) {
				t.Fatal("the cause is lost")
			}
		})
	}
	if wrap(nil) != nil {
		t.Fatal("wrap(nil) is not nil")
	}
}

// A class the game data lacks is the world's own fault, so internal, though character's Create refuses with the same rule.
func TestAWorldMissingAClassIsInternal(t *testing.T) {
	if got := failure.PublicOf(worldErr(kernel.ErrClassUnavailable)); got != failure.Internal {
		t.Fatalf("the world's class_unavailable is %s", got.Name)
	}
	if got := failure.PublicOf(wrap(kernel.ErrClassUnavailable)); got != failure.ClassUnavailable {
		t.Fatalf("character's class_unavailable is %s", got.Name)
	}
	if got := failure.PublicOf(worldErr(kernel.ErrNotEnoughEnergy)); got != failure.NotEnoughEnergy {
		t.Fatalf("another world rule is %s", got.Name)
	}
}
