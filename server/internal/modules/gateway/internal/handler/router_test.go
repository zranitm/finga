package handler_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"finga/modules/gateway/internal/handler"
)

// named answers with its own name, so a test sees which handler a path reached.
func named(name string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(name)) })
}

// restAPI mounts one REST route that answers "api".
func restAPI(e *gin.Engine) { e.POST("/api/accounts", gin.WrapH(named("api"))) }

func TestRouter(t *testing.T) {
	routes := handler.Routes{API: restAPI, Gateway: named("gateway")}
	tests := []struct {
		name     string
		routes   handler.Routes
		method   string
		path     string
		wantCode int
		wantBody string // a substring of the body
	}{
		{name: "rest_under_api", routes: routes, method: "POST", path: "/api/accounts", wantCode: http.StatusOK, wantBody: "api"},
		{name: "gateway_at_ws", routes: routes, method: "GET", path: "/ws", wantCode: http.StatusOK, wantBody: "gateway"},
		{name: "gateway_is_get_only", routes: routes, method: "POST", path: "/ws", wantCode: http.StatusMethodNotAllowed},
		{name: "no_gateway_mounts_nothing", routes: handler.Routes{API: restAPI}, method: "GET", path: "/ws", wantCode: http.StatusNotFound},
		{name: "no_metrics", routes: routes, method: "GET", path: "/metrics", wantCode: http.StatusNotFound},
		{name: "no_healthz", routes: routes, method: "GET", path: "/healthz", wantCode: http.StatusNotFound},
		{name: "no_readyz", routes: routes, method: "GET", path: "/readyz", wantCode: http.StatusNotFound},
		{name: "unknown_path", routes: routes, method: "GET", path: "/nope", wantCode: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
