package model_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"finga/internal/kernel"
	"finga/pkg/sentinels"
	"finga/services/gateway/internal/model"
)

func TestPublicOf(t *testing.T) {
	refused := &model.ServiceError{Public: model.Public{Code: "party_full", Status: 409, Message: "the party is full"}, Err: errors.New("rpc error")}
	tests := []struct {
		name string
		err  error
		want model.Public
	}{
		{"nil is the zero value", nil, model.Public{}},
		{"a service's failure is what its adapter named", fmt.Errorf("invite: %w", refused), refused.Public},
		{"a malformed ID from the gateway's own parsing", fmt.Errorf("item: %w", kernel.ErrBadID), model.BadID},
		{"an unknown class from the gateway's own parsing", kernel.ErrUnknownClass, model.UnknownClass},
		{"anything else is internal", errors.New("dial tcp: connection refused"), model.Internal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := model.PublicOf(tt.err); got != tt.want {
				t.Fatalf("PublicOf = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// TestEveryKernelErrorIsMapped reads the source of the shared kernel, whose
// errors the gateway's own parsing raises: an exported Err variable that
// PublicOf leaves out would reach a player as "something went wrong" with
// code internal. The services' errors are checked by their own handlers'
// guards and errmap's TestEveryReasonIsMapped.
func TestEveryKernelErrorIsMapped(t *testing.T) {
	declared, err := sentinels.Declared(filepath.Join("..", "..", "..", "..", "internal", "kernel"))
	if err != nil || len(declared) == 0 {
		t.Fatalf("declared %v, err %v: is the walk broken?", declared, err)
	}
	mapped, err := sentinels.Referenced("public.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range declared {
		if !mapped[name] {
			t.Errorf("%s has no row in PublicOf", name)
		}
	}
}

// A ServiceError reports its kind and its cause to errors.Is.
func TestAServiceErrorUnwrapsToItsKindAndCause(t *testing.T) {
	cause := errors.New("rpc error: unavailable")
	err := fmt.Errorf("join: %w", &model.ServiceError{Public: model.Internal, Kind: model.ErrUnavailable, Err: cause})
	if !errors.Is(err, model.ErrUnavailable) || !errors.Is(err, cause) || errors.Is(err, model.ErrNotSubscribed) {
		t.Fatal("the kind or the cause is lost")
	}
	if err.Error() != "join: rpc error: unavailable" {
		t.Fatalf("message %q", err.Error())
	}
}
