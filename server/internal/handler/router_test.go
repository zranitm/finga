package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"finga/internal/handler"
)

// named answers with its own name, so a test sees which handler a path reached.
func named(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(name)) })
}

func TestRouter(t *testing.T) {
	ready := false
	routes := handler.Routes{
		API:     named("api"),
		Gateway: named("gateway"),
		Metrics: named("metrics"),
		Ready:   func() bool { return ready },
	}
	tests := []struct {
		name     string
		routes   handler.Routes
		ready    bool
		method   string
		path     string
		wantCode int
		wantBody string // a substring of the body
	}{
		{name: "rest_under_api", routes: routes, method: "POST", path: "/api/accounts", wantCode: http.StatusOK, wantBody: "api"},
		{name: "gateway_at_ws", routes: routes, method: "GET", path: "/ws", wantCode: http.StatusOK, wantBody: "gateway"},
		{name: "gateway_is_get_only", routes: routes, method: "POST", path: "/ws", wantCode: http.StatusMethodNotAllowed},
		{name: "no_gateway_mounts_nothing", routes: handler.Routes{API: named("api"), Metrics: named("metrics"), Ready: func() bool { return true }}, method: "GET", path: "/ws", wantCode: http.StatusNotFound},
		{name: "metrics", routes: routes, method: "GET", path: "/metrics", wantCode: http.StatusOK, wantBody: "metrics"},
		{name: "healthz_is_always_200", routes: routes, method: "GET", path: "/healthz", wantCode: http.StatusOK, wantBody: `"ok"`},
		{name: "readyz_is_503_when_not_ready", routes: routes, method: "GET", path: "/readyz", wantCode: http.StatusServiceUnavailable, wantBody: `"not ready"`},
		{name: "readyz_is_200_when_ready", routes: routes, ready: true, method: "GET", path: "/readyz", wantCode: http.StatusOK, wantBody: `"ready"`},
		{name: "unknown_path", routes: routes, method: "GET", path: "/nope", wantCode: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ready = tt.ready
			rec := httptest.NewRecorder()
			handler.NewRouter(tt.routes).ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.wantCode {
				t.Fatalf("%s %s: status %d, want %d (body %q)", tt.method, tt.path, rec.Code, tt.wantCode, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Errorf("%s %s: body %q, want it to contain %q", tt.method, tt.path, rec.Body.String(), tt.wantBody)
			}
		})
	}
}
