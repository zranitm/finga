// Package handler holds the gateway's transports: api (REST) and ws (the
// WebSocket), on one Gin engine (TD-26). This file is the route table, the
// one place that lists every path the gateway answers.
package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"finga/pkg/runner"
)

// Routes are what the router mounts.
type Routes struct {
	API     func(e *gin.Engine) // mounts the REST API under /api, with its middleware and not-found answers
	Gateway http.Handler        // GET /ws; nil mounts nothing
	Metrics http.Handler        // GET /metrics
	Ready   func() bool         // GET /readyz: whether this process can take traffic now
}

// NewRouter returns the gateway's one handler: a Gin engine with the REST
// API under /api/, the gateway at /ws, /metrics, and the two probes.
// /healthz answers while the process runs; /readyz follows Ready and
// checks no dependency.
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
