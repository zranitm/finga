package transport

import (
	"encoding/json"
	"finga/internal/shared/failure"

	"github.com/gin-gonic/gin"
)

const (
	refusalKey = "finga.refusal"
	causeKey   = "finga.internal_cause"
)

// WriteError answers err as failure.PublicOf maps it; the error's text never reaches the body, only the request log.
func WriteError(c *gin.Context, err error) {
	p := failure.PublicOf(err)
	if failure.IsInternal(p) || p == failure.Unavailable {
		c.Set(causeKey, err)
	}
	WritePublic(c, p)
}

// WritePublic sends p and notes it for the request log.
func WritePublic(c *gin.Context, p failure.Public) {
	c.Set(refusalKey, p)
	WriteJSON(c, status(p), ErrorResponse{Code: p.Code, Message: p.Message})
}

// WriteJSON sends body as exactly application/json; Gin's renderer would add a charset.
func WriteJSON(c *gin.Context, status int, body any) {
	c.Header("Content-Type", "application/json")
	c.Status(status)
	_ = json.NewEncoder(c.Writer).Encode(body)
}

// ErrorResponse is every error body: a stable six-digit code, which the client words itself, and a message for developers.
type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
