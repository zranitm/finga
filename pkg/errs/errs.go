// Package errs is the error vocabulary shared by every layer: a typed error
// that carries a code and a message safe to show players. Modules declare
// their errors as package-level values (var ErrNotFound = errs.New(...)),
// return them for rule violations, and attach a cause with Wrap where a
// lower-level error explains the failure to an operator. Handlers map the
// code to an HTTP status or a protocol error code with CodeOf and send the
// player MessageOf; neither ever includes a wrapped error's text.
package errs

import (
	"errors"
)

// Code names an error class. Generic codes are below; a module adds its own
// domain codes (inventory_full, idempotency_key_reused) as plain strings.
type Code string

// The generic codes (Low-level design, page 4).
const (
	InvalidArgument Code = "invalid_argument"
	Unauthenticated Code = "unauthenticated"
	NotFound        Code = "not_found"
	Conflict        Code = "conflict"
	RateLimited     Code = "rate_limited"
	Unavailable     Code = "unavailable"
	Internal        Code = "internal"
)

// InternalMessage is what a player sees for an error that carries no code.
const InternalMessage = "something went wrong"

// Error is an error with a code and a player-safe message. Message never
// contains user input or a lower-level error's text; the cause, if any, is
// only reachable through Unwrap and Error, which are for logs.
type Error struct {
	Code    Code
	Message string
	cause   error
}

// New makes an error value with a code and a player-safe message.
func New(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// Invalid is New(InvalidArgument, message).
func Invalid(message string) *Error {
	return New(InvalidArgument, message)
}

// Wrap returns a copy of e with cause attached. e itself is unchanged, so a
// module's sentinel can be wrapped at every call site, and the copy still
// satisfies errors.Is(copy, sentinel).
func (e *Error) Wrap(cause error) *Error {
	return &Error{Code: e.Code, Message: e.Message, cause: cause}
}

// Error is for logs: the code, the message, and the cause when there is one.
func (e *Error) Error() string {
	s := string(e.Code) + ": " + e.Message
	if e.cause != nil {
		s += ": " + e.cause.Error()
	}
	return s
}

// Unwrap exposes the cause to errors.Is and errors.As.
func (e *Error) Unwrap() error {
	return e.cause
}

// Is reports whether target is an Error with the same code and message, so
// a wrapped copy matches the sentinel it was made from.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && t.Code == e.Code && t.Message == e.Message
}

// CodeOf is the code of the outermost Error in err's chain, Internal for an
// error without one, and empty for nil.
func CodeOf(err error) Code {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return Internal
}

// MessageOf is the player-safe message of the outermost Error in err's
// chain, InternalMessage for an error without one, and empty for nil.
func MessageOf(err error) string {
	if err == nil {
		return ""
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Message
	}
	return InternalMessage
}
