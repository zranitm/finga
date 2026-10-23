// Package handler holds the game server's transports: api (REST), ws (the
// gateway) and errmap (the error table both share). This file is the route
// table, the one place that lists every path the game server answers.
package handler

import "net/http"

// Routes are the handlers the router mounts.
type Routes struct {
	API     http.Handler // /api/
	Gateway http.Handler // GET /ws; nil mounts nothing
	Metrics http.Handler // GET /metrics
	Ready   func() bool  // GET /readyz: whether this process can take traffic now
}

// NewRouter returns the game server's one handler: REST under /api/, the
// gateway at /ws, /metrics, and the two probes. /healthz answers while the
// process runs; /readyz follows Ready and checks no dependency.
func NewRouter(r Routes) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/", r.API)
	if r.Gateway != nil {
		mux.Handle("GET /ws", r.Gateway)
	}
	mux.Handle("GET /metrics", r.Metrics)
	mux.HandleFunc("GET /healthz", healthz)
	mux.HandleFunc("GET /readyz", readyz(r.Ready))
	return mux
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

func readyz(ready func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !ready() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"not ready"}` + "\n"))
			return
		}
		_, _ = w.Write([]byte(`{"status":"ready"}` + "\n"))
	}
}
