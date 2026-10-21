package api

import (
	"encoding/json"
	"net/http"

	"finga/pkg/errs"
)

// statusFor maps an error code to an HTTP status. Generic codes have their
// own; a domain code (inventory_full, idempotency_key_reused) is a rule the
// request ran into, which is a conflict with the current state.
func statusFor(code errs.Code) int {
	switch code {
	case errs.InvalidArgument:
		return http.StatusBadRequest
	case errs.Unauthenticated:
		return http.StatusUnauthorized
	case errs.NotFound:
		return http.StatusNotFound
	case errs.Conflict:
		return http.StatusConflict
	case errs.RateLimited:
		return http.StatusTooManyRequests
	case errs.Unavailable:
		return http.StatusServiceUnavailable
	case errs.Internal:
		return http.StatusInternalServerError
	default:
		return http.StatusConflict
	}
}

// writeError sends the error's code and player message. Nothing of a
// wrapped cause reaches the body; the request log carries the details.
func writeError(w http.ResponseWriter, err error) {
	writeCode(w, statusFor(errs.CodeOf(err)), errs.CodeOf(err), errs.MessageOf(err))
}

func writeCode(w http.ResponseWriter, status int, code errs.Code, message string) {
	writeJSON(w, status, ErrorResponse{Code: string(code), Message: message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
