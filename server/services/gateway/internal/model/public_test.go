package model_test

import (
	"errors"
	"fmt"
	"testing"

	"finga/internal/kernel"
	"finga/services/gateway/internal/model"
)

func TestPublicOf(t *testing.T) {
	refused := &model.ServiceError{Public: model.PartyFull, Err: errors.New("rpc error")}
	tests := []struct {
		name string
		err  error
		want model.Public
	}{
		{"nil is the zero value", nil, model.Public{}},
		{"a service's failure is what its adapter named", fmt.Errorf("invite: %w", refused), refused.Public},
		{"a malformed ID from the gateway's own parsing", fmt.Errorf("item: %w", kernel.ErrBadID), model.BadID},
		{"an unknown class from the gateway's own parsing", kernel.ErrUnknownClass, model.UnknownClass},
		{"a Hello whose token or character is not the account's", fmt.Errorf("hello: %w", model.ErrHelloRejected), model.Unauthenticated},
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
