package runner

import "net/http"

// Probes serves a process's operational endpoints: /healthz while it runs,
// /readyz following ready, and /metrics.
func Probes(ready func() bool, metrics http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", Healthz)
	mux.HandleFunc("GET /readyz", Readyz(ready))
	mux.Handle("GET /metrics", metrics)
	return mux
}

// Healthz answers while the process runs.
func Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}` + "\n"))
}

// Readyz answers 200 while ready says so and 503 otherwise; it checks no
// dependency.
func Readyz(ready func() bool) http.HandlerFunc {
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
