// Package model is the gateway's model (Low-level design, page 11). The
// gateway's job is the conversation with clients, so the client protocol's
// messages (internal/protocol) are its vocabulary for what a player sees
// and sends; this package adds what the gateway keeps around them: a
// failure as a client sees it, the envelope of a message, a connection's
// player and limits, the REST API's accounts and characters, and the
// events of the services' streams. It needs nothing but data.
package model

import (
	"errors"
	"net/http"

	"finga/internal/kernel"
)

// Public is a failure as a client sees it: a stable code of the gateway's
// own (party_full, inventory_full), the HTTP status, and a message safe to
// show a player. The message is written by the gateway, never taken from
// an error, so a cause's text cannot reach a client. The WebSocket sends
// only Code and Message, in a Response.
type Public struct {
	Code    string `json:"code"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// The failures the gateway raises itself, before or around a service call.
var (
	MalformedBody        = Public{"malformed_body", http.StatusBadRequest, "malformed request body"}
	MissingField         = Public{"missing_field", http.StatusBadRequest, "missing a required field"}
	BodyTooLarge         = Public{"body_too_large", http.StatusRequestEntityTooLarge, "request body too large"}
	NoSuchEndpoint       = Public{"no_such_endpoint", http.StatusNotFound, "no such endpoint"}
	MethodNotAllowed     = Public{"method_not_allowed", http.StatusMethodNotAllowed, "method not allowed"}
	RateLimited          = Public{"rate_limited", http.StatusTooManyRequests, "slow down"}
	NotImplemented       = Public{"not_implemented", http.StatusNotImplemented, "not available yet"}
	ChatUnavailable      = Public{"chat_unavailable", http.StatusServiceUnavailable, "chat is not available"}
	PartiesUnavailable   = Public{"parties_unavailable", http.StatusServiceUnavailable, "parties are not available"}
	InventoryUnavailable = Public{"inventory_unavailable", http.StatusServiceUnavailable, "the inventory is not available"}
	// Unauthenticated is a missing or malformed token, raised by the
	// gateway, and the account service's refusal of a token or a login.
	Unauthenticated = Public{"unauthenticated", http.StatusUnauthorized, "invalid credentials or session"}
	// BadID and UnknownClass are the gateway's own parsing failing, and the
	// services' refusals of the same.
	BadID        = Public{"bad_id", http.StatusBadRequest, "malformed ID"}
	UnknownClass = Public{"unknown_class", http.StatusBadRequest, "unknown class"}
	Internal     = Public{"internal", http.StatusInternalServerError, "something went wrong"}
)

// Raised are the failures the gateway raises itself, for the list of every
// code a client can receive.
func Raised() []Public {
	return []Public{
		MalformedBody, MissingField, BodyTooLarge, NoSuchEndpoint, MethodNotAllowed, RateLimited,
		NotImplemented, ChatUnavailable, PartiesUnavailable, InventoryUnavailable, Unauthenticated,
		BadID, UnknownClass, Internal,
	}
}

// CharacterNotFound is the code of a character that is not the account's
// (or not there at all): a Hello that names one is unauthenticated.
const CharacterNotFound = "character_not_found"

// The kinds of service failure the gateway acts on itself.
var (
	// ErrUnavailable is a service that cannot be reached: starting,
	// restarting, or down.
	ErrUnavailable = errors.New("gateway: service unavailable")
	// ErrNotSubscribed is a join refused because the world has not seen this
	// gateway's delivery stream yet (or lost it).
	ErrNotSubscribed = errors.New("gateway: the world does not hold this gateway's stream")
)

// ServiceError is a service's failure as the gateway's adapters hand it
// over: what a client sees, the kind the gateway acts on (nil when none),
// and the cause, for the logs.
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

// kernelErrors are the shared kernel's errors the gateway's own parsing
// raises (an item ID in a message, a class in a request).
var kernelErrors = []struct {
	err error
	pub Public
}{
	{kernel.ErrBadID, BadID},
	{kernel.ErrUnknownClass, UnknownClass},
}

// PublicOf is err as a client sees it: a service's failure as its adapter
// named it, the gateway's own parsing failures, and Internal for anything
// else (which the caller logs with its cause); the zero Public for nil.
func PublicOf(err error) Public {
	if err == nil {
		return Public{}
	}
	var se *ServiceError
	if errors.As(err, &se) {
		return se.Public
	}
	for _, row := range kernelErrors {
		if errors.Is(err, row.err) {
			return row.pub
		}
	}
	return Internal
}

// IsInternal reports whether p answers an error nobody expected, which the
// caller logs with its cause.
func IsInternal(p Public) bool { return p.Code == Internal.Code }
