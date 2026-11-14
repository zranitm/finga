package local

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"finga/internal/kernel"
	"finga/pkg/transient"
	"finga/services/gateway/internal/model"
)

// A module's failure reaches the use cases with the Public and the kind errmap.Wrap gave the same failure over gRPC.
func TestWrapNamesWhatTheUseCasesActOn(t *testing.T) {
	tests := []struct {
		name string
		err  error
		kind error // nil: none
		pub  model.Public
	}{
		{"a final rule is its Public", fmt.Errorf("invite: %w", kernel.ErrPartyFull), nil, model.PartyFull},
		{"the world not holding the stream", kernel.ErrNotSubscribed, model.ErrNotSubscribed, model.Internal},
		{"a module down for now", fmt.Errorf("%w: pool closed", transient.ErrUnavailable), model.ErrUnavailable, model.Unavailable},
		{"a world too busy to take a join", kernel.ErrMapBusy, model.ErrUnavailable, model.Unavailable},
		{"a world that is stopping", kernel.ErrWorldStopped, model.ErrUnavailable, model.Unavailable},
		{"a module's own failure", kernel.ErrInternal, nil, model.Internal},
		{"a caller that gave up", context.Canceled, nil, model.Internal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := wrap(tt.err)
			if got := model.PublicOf(err); got != tt.pub {
				t.Fatalf("PublicOf = %+v, want %s", got, tt.pub.Name)
			}
			for _, kind := range []error{model.ErrNotSubscribed, model.ErrUnavailable} {
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

// A class the game data lacks is the world's own fault, as errmap read the world's reason, though character's Create refuses with it.
func TestAWorldMissingAClassIsInternal(t *testing.T) {
	if got := model.PublicOf(worldErr(kernel.ErrClassUnavailable)); got != model.Internal {
		t.Fatalf("the world's class_unavailable is %s", got.Name)
	}
	if got := model.PublicOf(wrap(kernel.ErrClassUnavailable)); got != model.ClassUnavailable {
		t.Fatalf("character's class_unavailable is %s", got.Name)
	}
	if got := model.PublicOf(worldErr(kernel.ErrNotEnoughEnergy)); got != model.NotEnoughEnergy {
		t.Fatalf("another world rule is %s", got.Name)
	}
}
