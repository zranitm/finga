package errs_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"finga/pkg/errs"
)

var (
	errNotFound = errs.New(errs.NotFound, "no such item")
	errFull     = errs.New("inventory_full", "your inventory is full")
)

func TestCodeOf(t *testing.T) {
	cause := errors.New("pq: relation items does not exist")
	tests := []struct {
		name string
		err  error
		want errs.Code
	}{
		{"nil has no code", nil, ""},
		{"a plain error is internal", cause, errs.Internal},
		{"a value keeps its code", errNotFound, errs.NotFound},
		{"a domain code is kept as is", errFull, "inventory_full"},
		{"Wrap keeps the code", errNotFound.Wrap(cause), errs.NotFound},
		{"fmt wrapping keeps the code", fmt.Errorf("pickup: %w", errNotFound), errs.NotFound},
		{"two levels of wrapping keep the code", fmt.Errorf("ws: %w", fmt.Errorf("pickup: %w", errFull.Wrap(cause))), "inventory_full"},
		{"the outermost errs value wins", fmt.Errorf("x: %w", errs.New(errs.Conflict, "taken").Wrap(errNotFound)), errs.Conflict},
		{"Invalid is invalid_argument", errs.Invalid("name too short"), errs.InvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errs.CodeOf(tt.err); got != tt.want {
				t.Fatalf("CodeOf = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestErrorsAs(t *testing.T) {
	cause := errors.New("deadline exceeded")
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"direct value", errNotFound, true},
		{"wrapped with fmt", fmt.Errorf("ctx: %w", errNotFound), true},
		{"wrapped with Wrap", errFull.Wrap(cause), true},
		{"plain error", cause, false},
		{"joined errors", errors.Join(cause, errNotFound), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var e *errs.Error
			if got := errors.As(tt.err, &e); got != tt.want {
				t.Fatalf("errors.As = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorsIs(t *testing.T) {
	cause := errors.New("boom")
	tests := []struct {
		name   string
		err    error
		target error
		want   bool
	}{
		{"same value", errNotFound, errNotFound, true},
		{"Wrap keeps identity with the sentinel", errNotFound.Wrap(cause), errNotFound, true},
		{"fmt wrapping keeps identity", fmt.Errorf("x: %w", errNotFound.Wrap(cause)), errNotFound, true},
		{"the cause stays reachable", errNotFound.Wrap(cause), cause, true},
		{"a different code is not the sentinel", errFull, errNotFound, false},
		{"same code but a different message is a different error", errs.New(errs.NotFound, "no such character"), errNotFound, false},
		{"plain error is not a sentinel", cause, errNotFound, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.err, tt.target); got != tt.want {
				t.Fatalf("errors.Is = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessageOf(t *testing.T) {
	secret := "pq: duplicate key value violates unique constraint accounts_username_key"
	cause := errors.New(secret)
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil has no message", nil, ""},
		{"a plain error gets the generic message", cause, errs.InternalMessage},
		{"a value gives its message", errNotFound, "no such item"},
		{"Wrap keeps the player message", errNotFound.Wrap(cause), "no such item"},
		{"fmt wrapping keeps the player message", fmt.Errorf("ctx: %w", errFull.Wrap(cause)), "your inventory is full"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := errs.MessageOf(tt.err)
			if got != tt.want {
				t.Fatalf("MessageOf = %q, want %q", got, tt.want)
			}
			if strings.Contains(got, secret) {
				t.Fatalf("player message leaks the cause: %q", got)
			}
		})
	}
}

func TestErrorText(t *testing.T) {
	cause := errors.New("connection refused")
	tests := []struct {
		name         string
		err          *errs.Error
		wantContains []string
	}{
		{"value names code and message", errNotFound, []string{"not_found", "no such item"}},
		{"wrapped value also names the cause, for logs", errNotFound.Wrap(cause), []string{"not_found", "no such item", "connection refused"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := tt.err.Error()
			for _, want := range tt.wantContains {
				if !strings.Contains(text, want) {
					t.Fatalf("Error() = %q, want it to contain %q", text, want)
				}
			}
			if strings.Contains(tt.err.Message, "connection refused") {
				t.Fatalf("Message leaks the cause: %q", tt.err.Message)
			}
		})
	}
}

func TestWrapDoesNotMutateTheSentinel(t *testing.T) {
	cause := errors.New("boom")
	wrapped := errNotFound.Wrap(cause)
	if errors.Unwrap(errNotFound) != nil {
		t.Fatal("Wrap changed the sentinel's cause")
	}
	if errors.Unwrap(wrapped) != cause { //nolint:errorlint // identity of the direct cause is the point
		t.Fatal("wrapped error does not unwrap to the cause")
	}
	if wrapped == errNotFound {
		t.Fatal("Wrap returned the sentinel itself")
	}
}
