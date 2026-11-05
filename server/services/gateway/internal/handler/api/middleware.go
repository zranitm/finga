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
	"finga/pkg/reqid"
	"finga/pkg/uuidv7"
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
	headerReqID    = "X-Request-ID"
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
			c.Header("Access-Control-Expose-Headers", headerReqID)
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

// requestLog gives each request an ID, which the response names, and logs the request without headers or
// bodies (no tokens or passwords). It recovers panics as 500; http.ErrAbortHandler goes on to net/http,
// which drops the connection.
func requestLog(log *slog.Logger, rec *panics.Recorder) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		id := uuidv7.NewString()
		c.Request = c.Request.WithContext(reqid.With(c.Request.Context(), id))
		c.Header(headerReqID, id)
		defer func() {
			r := c.Request
			p := recover()
			aborted := p == http.ErrAbortHandler //nolint:errorlint // net/http compares the sentinel itself
			if p != nil && !aborted {
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
			if aborted {
				args = append(args, "aborted", true)
			}
			log.Log(r.Context(), level, "request", args...)
			if aborted {
				panic(p)
			}
		}()
		c.Next()
	}
}
