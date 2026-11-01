// Package handler mounts the gateway's transports, REST (api) and WebSocket (ws), on one Gin engine.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"finga/pkg/runner"
)

// Routes are what the router mounts.
type Routes struct {
	API     func(e *gin.Engine) // mounts /api
	Gateway http.Handler        // GET /ws; nil mounts nothing
	Metrics http.Handler        // GET /metrics
	Ready   func() bool         // GET /readyz
}

// NewRouter returns the gateway's one handler: /api, /ws, /metrics, /healthz and /readyz.
func NewRouter(r Routes) http.Handler {
	e := NewEngine()
	if r.API != nil {
		r.API(e)
	}
	if r.Gateway != nil {
		e.GET("/ws", gin.WrapH(r.Gateway))
	}
	e.GET("/metrics", gin.WrapH(r.Metrics))
	e.GET("/healthz", gin.WrapF(runner.Healthz))
	e.GET("/readyz", gin.WrapF(runner.Readyz(r.Ready)))
	return e
}
