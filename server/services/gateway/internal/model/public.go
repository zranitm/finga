// Package model holds the gateway's use-case data: client-facing failures, command inputs and deliveries.
package model

import (
	"errors"
	"net/http"

	"finga/internal/kernel"
)

// Public is a failure as a client sees it; Message is the gateway's own, never an error's text, so causes never leak.
type Public struct {
	Code    string `json:"code"`
	Status  int    `json:"status"`
	Message string `json:"message"`
}

// The failures the gateway raises itself.
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
	Unauthenticated      = Public{"unauthenticated", http.StatusUnauthorized, "invalid credentials or session"}
	BadID                = Public{"bad_id", http.StatusBadRequest, "malformed ID"}
	UnknownClass         = Public{"unknown_class", http.StatusBadRequest, "unknown class"}
	Internal             = Public{"internal", http.StatusInternalServerError, "something went wrong"}
)

// Raised lists the failures the gateway raises itself.
func Raised() []Public {
	return []Public{
		MalformedBody, MissingField, BodyTooLarge, NoSuchEndpoint, MethodNotAllowed, RateLimited,
		NotImplemented, ChatUnavailable, PartiesUnavailable, InventoryUnavailable, Unauthenticated,
		BadID, UnknownClass, Internal,
	}
}

// CharacterNotFound is the code of a character that is not the account's; a Hello naming one is rejected.
const CharacterNotFound = "character_not_found"

// The failures of the gateway's own use cases.
var (
	ErrNoInventory   = errors.New("gateway: no item service")
	ErrNoParties     = errors.New("gateway: no social service, so no parties")
	ErrNoChat        = errors.New("gateway: no social service, so no chat")
	ErrHelloRejected = errors.New("gateway: hello rejected")
)

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
	{ErrNoInventory, InventoryUnavailable},
	{ErrNoParties, PartiesUnavailable},
	{ErrNoChat, ChatUnavailable},
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
