package api

import (
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"finga/pkg/auth"
	"finga/pkg/panics"
	"finga/services/gateway/internal/application"
	"finga/services/gateway/internal/model"
)

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

func principalFrom(c *gin.Context) auth.Principal {
	v, _ := c.Get(principalKey)
	p, _ := v.(auth.Principal)
	return p
}

// requireAuth resolves the bearer token; any bad token is 401 unauthenticated.
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

// cors answers allowed origins and refuses other preflights; with none allowed, same-origin only.
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

// requestLog logs each request without headers or bodies (no tokens or passwords) and recovers panics as 500.
func requestLog(log *slog.Logger, rec *panics.Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		defer func() {
			r := c.Request
			if p := recover(); p != nil {
				if !c.Writer.Written() {
					writePublic(c, model.Internal)
				}
				c.Abort()
				rec.Record(r.Context(), panics.FlowHTTP, panics.Of(p), "method", r.Method, "path", r.URL.Path)
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
