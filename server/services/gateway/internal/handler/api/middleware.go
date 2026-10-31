package api

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"finga/pkg/auth"
	"finga/services/gateway/internal/application"
	"finga/services/gateway/internal/model"
)

// principalKey is the request context's key for the session the auth
// middleware resolved.
const principalKey = "finga.principal"

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
func principalFrom(c *gin.Context) auth.Principal {
	v, _ := c.Get(principalKey)
	p, _ := v.(auth.Principal)
	return p
}

// requireAuth resolves the bearer token to a principal through the Lobby.
// Missing, malformed, unknown, expired and revoked tokens all give 401
// unauthenticated; any other failure is 500.
func requireAuth(lobby *application.Lobby) gin.HandlerFunc {
	return func(c *gin.Context) {
		scheme, token, ok := strings.Cut(c.GetHeader("Authorization"), " ")
		token = strings.TrimSpace(token)
		if !ok || !strings.EqualFold(scheme, bearerScheme) || token == "" {
			c.Header("WWW-Authenticate", "Bearer")
			writePublic(c, model.Unauthenticated)
			c.Abort()
			return
		}
		p, err := lobby.Authenticate(c.Request.Context(), token)
		if err != nil {
			if model.PublicOf(err) == model.Unauthenticated {
				c.Header("WWW-Authenticate", "Bearer")
			}
			writeError(c, err)
			c.Abort()
			return
		}
		c.Set(principalKey, p)
		c.Next()
	}
}

// cors answers browsers from the allowed origins and refuses preflights
// from anywhere else. With no allowed origins, same-origin only.
func cors(origins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader(headerOrigin)
		allowed := origin != "" && slices.Contains(origins, origin)
		if allowed {
			c.Header(headerAllowOrg, origin)
			c.Writer.Header().Add(headerVary, headerOrigin)
		}
		if c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != "" {
			if !allowed {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			c.Header("Access-Control-Allow-Methods", corsMethods)
			c.Header("Access-Control-Allow-Headers", corsHeaders)
			c.Header("Access-Control-Max-Age", strconv.Itoa(corsMaxAge))
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

// requestLog logs one line per request: method, path, status and
// duration, the error code of a refusal, and the cause of an internal
// error at level ERROR. Never headers or bodies, so tokens and passwords
// stay out. A panic is logged with its stack and answered with 500
// internal; it is the API's recovery, in place of Gin's own.
func requestLog(log *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		defer func() {
			r := c.Request
			if p := recover(); p != nil {
				if !c.Writer.Written() {
					writePublic(c, model.Internal)
				}
				c.Abort()
				log.ErrorContext(r.Context(), "panic in request", "method", r.Method, "path", r.URL.Path, "panic", p, "stack", string(debug.Stack()))
			}
			args := []any{"method", r.Method, "path", r.URL.Path, "status", c.Writer.Status(), "duration_ms", float64(time.Since(start).Microseconds()) / 1000}
			level := slog.LevelInfo
			if code := c.GetString(codeKey); code != "" {
				args = append(args, "code", code)
			}
			if v, ok := c.Get(causeKey); ok {
				if err, ok := v.(error); ok {
					args = append(args, "err", err.Error())
					level = slog.LevelError
				}
			}
			log.Log(r.Context(), level, "request", args...)
		}()
		c.Next()
	}
}
