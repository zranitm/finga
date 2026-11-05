// Package handler mounts the gateway's transports, REST (api) and WebSocket (ws), on one Gin engine.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Routes are what the router mounts.
type Routes struct {
	API     func(e *gin.Engine) // mounts /api
	Gateway http.Handler        // GET /ws; nil mounts nothing
}

// NewRouter returns the handler players reach: /api and /ws. The probes and /metrics are on the ops listener.
func NewRouter(r Routes) http.Handler {
	e := NewEngine()
	if r.API != nil {
		r.API(e)
	}
	if r.Gateway != nil {
		e.GET("/ws", gin.WrapH(r.Gateway))
	}
	return e
}
