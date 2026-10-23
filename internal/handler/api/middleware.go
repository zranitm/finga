package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"finga/internal/application/account"
	"finga/internal/handler/errmap"
	"finga/pkg/auth"
)

type ctxKey int

const principalKey ctxKey = iota

const (
	bearerScheme   = "bearer"
	corsMaxAge     = 600 // seconds a preflight answer may be cached
	corsMethods    = "GET, POST, PUT, DELETE, OPTIONS"
	corsHeaders    = "Authorization, Content-Type"
	headerOrigin   = "Origin"
	headerVary     = "Vary"
	headerAllowOrg = "Access-Control-Allow-Origin"
)

// principalFrom is the session the auth middleware resolved.
func principalFrom(ctx context.Context) auth.Principal {
	p, _ := ctx.Value(principalKey).(auth.Principal)
	return p
}

// requireAuth resolves the bearer token to a principal through the
// Sessions interface. Missing, malformed, unknown, expired and revoked
// tokens all give 401 unauthenticated; a store failure is 500.
func requireAuth(sessions auth.Sessions, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
		token = strings.TrimSpace(token)
		if !ok || !strings.EqualFold(scheme, bearerScheme) || token == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeError(w, account.ErrUnauthenticated)
			return
		}
		p, err := sessions.Validate(r.Context(), token)
		if err != nil {
			if errors.Is(err, account.ErrUnauthenticated) {
				w.Header().Set("WWW-Authenticate", "Bearer")
			}
			writeError(w, err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey, p)))
	})
}

// withCORS answers browsers from the allowed origins and refuses preflights
// from anywhere else. With no allowed origins, same-origin only.
func withCORS(origins []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get(headerOrigin)
		allowed := origin != "" && slices.Contains(origins, origin)
		if allowed {
			w.Header().Set(headerAllowOrg, origin)
			w.Header().Add(headerVary, headerOrigin)
		}
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if !allowed {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Header().Set("Access-Control-Allow-Methods", corsMethods)
			w.Header().Set("Access-Control-Allow-Headers", corsHeaders)
			w.Header().Set("Access-Control-Max-Age", strconv.Itoa(corsMaxAge))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// statusWriter remembers what the request log needs: the status, the code
// of a refusal, and the error behind an internal one.
type statusWriter struct {
	http.ResponseWriter
	status int
	code   string
	err    error
}

// WriteHeader records the first status written.
func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

// Write counts an implicit 200 when nothing was written before.
func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// withLogging logs one line per request: method, path, status and
// duration, the error code of a refusal, and the cause of an internal
// error at level ERROR. Never headers or bodies, so tokens and passwords
// stay out. A panic is logged with its stack and answered with 500
// internal.
func withLogging(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				if sw.status == 0 {
					writePublic(sw, errmap.Internal)
				}
				log.ErrorContext(r.Context(), "panic in request", "method", r.Method, "path", r.URL.Path, "panic", p, "stack", string(debug.Stack()))
			}
			args := []any{"method", r.Method, "path", r.URL.Path, "status", sw.status, "duration_ms", float64(time.Since(start).Microseconds()) / 1000}
			level := slog.LevelInfo
			if sw.code != "" {
				args = append(args, "code", sw.code)
			}
			if sw.err != nil {
				args = append(args, "err", sw.err.Error())
				level = slog.LevelError
			}
			log.Log(r.Context(), level, "request", args...)
		}()
		next.ServeHTTP(sw, r)
	})
}
