package api

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"finga/services/gateway/internal/model"
)

// The request context's keys the request log reads back.
const (
	codeKey  = "finga.refusal_code"   // the code of a refusal
	causeKey = "finga.internal_cause" // the error behind an internal one
)

// writeError answers a failure with its own code, status and player
// message (model.PublicOf). Nothing of the error's text reaches the body.
// The request log carries the code, and the cause too when the failure is
// not one the gateway knows.
func writeError(c *gin.Context, err error) {
	p := model.PublicOf(err)
	if model.IsInternal(p) {
		c.Set(causeKey, err)
	}
	writePublic(c, p)
}

// writePublic sends a failure as the client sees it and notes its code
// for the request log.
func writePublic(c *gin.Context, p model.Public) {
	c.Set(codeKey, p.Code)
	writeJSON(c, p.Status, ErrorResponse{Code: p.Code, Message: p.Message})
}

// writeJSON sends body with Content-Type application/json exactly (Gin's
// own JSON render adds a charset, which page 4's contract does not).
func writeJSON(c *gin.Context, status int, body any) {
	c.Header("Content-Type", "application/json")
	c.Status(status)
	_ = json.NewEncoder(c.Writer).Encode(body)
}
