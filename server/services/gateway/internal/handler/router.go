// Package handler holds the gateway's transports: api (REST), ws (the
// gateway) and errmap (the error table both share). This file is the route
// table, the one place that lists every path the gateway answers.
package handler

import (
	"net/http"

	"finga/pkg/runner"
)

// Routes are the handlers the router mounts.
type Routes struct {
	API     http.Handler // /api/
	Gateway http.Handler // GET /ws; nil mounts nothing
	Metrics http.Handler // GET /metrics
	Ready   func() bool  // GET /readyz: whether this process can take traffic now
}

// NewRouter returns the gateway's one handler: REST under /api/, the
// gateway at /ws, /metrics, and the two probes. /healthz answers while the
// process runs; /readyz follows Ready and checks no dependency.
func NewRouter(r Routes) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", r.API)
	if r.Gateway != nil {
		mux.Handle("GET /ws", r.Gateway)
	}
	mux.Handle("GET /metrics", r.Metrics)
	mux.HandleFunc("GET /healthz", runner.Healthz)
	mux.HandleFunc("GET /readyz", runner.Readyz(r.Ready))
	return mux
}
