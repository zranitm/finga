package api

import (
	"encoding/json"
	"net/http"

	"finga/internal/handler/errmap"
)

// writeError answers a service error with its own code, status and player
// message from errmap. Nothing of the error's text reaches the body. The
// request log carries the code, and the cause too when the error is not
// one the table knows.
func writeError(w http.ResponseWriter, err error) {
	p := errmap.For(err)
	if sw, ok := w.(*statusWriter); ok && errmap.IsInternal(p) {
		sw.err = err
	}
	writePublic(w, p)
}

// writePublic sends a failure as the client sees it and notes its code
// for the request log.
func writePublic(w http.ResponseWriter, p errmap.Public) {
	if sw, ok := w.(*statusWriter); ok {
		sw.code = p.Code
	}
	writeJSON(w, p.Status, ErrorResponse{Code: p.Code, Message: p.Message})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
