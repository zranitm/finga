package api

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"finga/services/gateway/internal/model"
)

const (
	codeKey  = "finga.refusal_code"
	causeKey = "finga.internal_cause"
)

// writeError answers err as model.PublicOf maps it; the error's text never reaches the body, only the request log.
func writeError(c *gin.Context, err error) {
	p := model.PublicOf(err)
	if model.IsInternal(p) || p == model.Unavailable {
		c.Set(causeKey, err)
	}
	writePublic(c, p)
}

// writePublic sends p and notes its code for the request log.
func writePublic(c *gin.Context, p model.Public) {
	c.Set(codeKey, p.Code)
	writeJSON(c, status(p), ErrorResponse{Code: p.Code, Message: p.Message})
}

// writeJSON sends body as exactly application/json; Gin's renderer would add a charset.
func writeJSON(c *gin.Context, status int, body any) {
	c.Header("Content-Type", "application/json")
	c.Status(status)
	_ = json.NewEncoder(c.Writer).Encode(body)
}
