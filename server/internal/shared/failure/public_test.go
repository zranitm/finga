package failure_test

import (
	"errors"
	"fmt"
	"testing"

	"finga/internal/shared/failure"
	"finga/internal/shared/kernel"
)

func TestPublicOf(t *testing.T) {
	refused := &failure.ServiceError{Public: failure.PartyFull, Err: errors.New("rpc error")}
	tests := []struct {
		name string
		err  error
		want failure.Public
	}{
		{"nil is the zero value", nil, failure.Public{}},
		{"a module's failure is what its adapter named", fmt.Errorf("invite: %w", refused), refused.Public},
		{"a malformed ID from the gateway's own parsing", fmt.Errorf("item: %w", kernel.ErrBadID), failure.BadID},
		{"an unknown class from the gateway's own parsing", kernel.ErrUnknownClass, failure.UnknownClass},
		{"a Hello whose token or character is not the account's", fmt.Errorf("hello: %w", failure.ErrHelloRejected), failure.Unauthenticated},
		{"anything else is internal", errors.New("dial tcp: connection refused"), failure.Internal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := failure.PublicOf(tt.err); got != tt.want {
				t.Fatalf("PublicOf = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// A ServiceError reports its kind and its cause to errors.Is.
func TestAServiceErrorUnwrapsToItsKindAndCause(t *testing.T) {
	cause := errors.New("rpc error: unavailable")
	err := fmt.Errorf("join: %w", &failure.ServiceError{Public: failure.Internal, Kind: failure.ErrUnavailable, Err: cause})
	if !errors.Is(err, failure.ErrUnavailable) || !errors.Is(err, cause) || errors.Is(err, failure.ErrNotSubscribed) {
		t.Fatal("the kind or the cause is lost")
	}
	if err.Error() != "join: rpc error: unavailable" {
		t.Fatalf("message %q", err.Error())
	}
}
