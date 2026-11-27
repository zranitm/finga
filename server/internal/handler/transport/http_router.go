// Package transport is what the module handlers share: the HTTP engine and its edge middleware, and the WebSocket's connection, read loop, outbox and encoders, and the routes and encoders each module's handler registers on them.
package transport

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// HTTPRoutes are what the router mounts.
type HTTPRoutes struct {
	API func(e *gin.Engine) // mounts /api
	WS  http.Handler        // GET /ws; nil mounts nothing
}

// NewRouter returns the handler players reach: /api and /ws. The probes and /metrics are on the ops listener.
func NewRouter(r HTTPRoutes) http.Handler {
	e := NewEngine()
	if r.API != nil {
		r.API(e)
	}
	if r.WS != nil {
		e.GET("/ws", gin.WrapH(r.WS))
	}
	return e
}
