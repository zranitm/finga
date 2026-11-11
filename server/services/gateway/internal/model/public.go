// Package model holds the gateway's use-case data: client-facing failures, command inputs and deliveries.
package model

import (
	"errors"

	"finga/internal/kernel"
)

// Public is a failure as a client sees it; Message is the gateway's own, never an error's text, so causes never leak.
type Public struct {
	Code    int    // six digits: the area, then the case (failures.go)
	Name    string // EQUIP_TAB_FULL: the code in source and in logs
	Message string
}

// ErrHelloRejected is a Hello the gateway refuses itself.
var ErrHelloRejected = errors.New("gateway: hello rejected")

// The kinds of service failure the gateway acts on itself.
var (
	ErrUnavailable   = errors.New("gateway: service unavailable")
	ErrNotSubscribed = errors.New("gateway: the world does not hold this gateway's stream")
)

// ServiceError is a service failure: what the client sees, the kind the gateway acts on (or nil), and the cause.
type ServiceError struct {
	Public Public
	Kind   error
	Err    error
}

func (e *ServiceError) Error() string { return e.Err.Error() }

// Unwrap exposes the kind and the cause to errors.Is and errors.As.
func (e *ServiceError) Unwrap() []error {
	if e.Kind != nil {
		return []error{e.Kind, e.Err}
	}
	return []error{e.Err}
}

var ownErrors = []struct {
	err error
	pub Public
}{
	{ErrHelloRejected, Unauthenticated},
	{kernel.ErrBadID, BadID},
	{kernel.ErrUnknownClass, UnknownClass},
}

// PublicOf is err as a client sees it: Internal for anything unexpected, the zero Public for nil.
func PublicOf(err error) Public {
	if err == nil {
		return Public{}
	}
	if se, ok := errors.AsType[*ServiceError](err); ok {
		return se.Public
	}
	for _, row := range ownErrors {
		if errors.Is(err, row.err) {
			return row.pub
		}
	}
	return Internal
}

// IsInternal reports whether p answers an unexpected error, which the caller should log.
func IsInternal(p Public) bool { return p.Code == Internal.Code }
