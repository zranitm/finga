// Package transport: the REST surface the module handlers add their routes to.
package transport

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel/trace"

	application "finga/internal/application/gateway"
	model "finga/internal/model/gateway"
	"finga/pkg/panics"
	"finga/pkg/ratelimit"
)

const (
	maxBodyBytes    = 64 << 10         // the largest legitimate body holds a 128-character password
	bodyReadTimeout = 10 * time.Second // from the end of the headers, which the server's own timeout covers
)

// RESTOptions are the use cases and settings the REST surface is built from.
type RESTOptions struct {
	Lobby       *application.Lobby
	Logger      *slog.Logger
	CORSOrigins []string
	// Panics records a panic in a request.
	Panics *panics.Recorder
	// Traces starts each request's trace.
	Traces trace.TracerProvider
	// Limit holds each account's REST calls to its bucket, keyed by account ID.
	Limit ratelimit.Limiter
}

// REST is the /api route groups a module handler adds its routes to: Public needs no login, Authed needs a session and counts against the account's bucket.
type REST struct {
	Public *gin.RouterGroup
	Authed *gin.RouterGroup
}

// MountREST serves the REST API under /api, each module's routes added by its handler; unrouted paths also pass the log and CORS, so any preflight is answered.
func MountREST(d RESTOptions, modules ...func(REST)) func(e *gin.Engine) {
	return func(e *gin.Engine) {
		trace := traced(d.Traces)
		edge := func(last gin.HandlerFunc) []gin.HandlerFunc {
			return []gin.HandlerFunc{trace, requestLog(d.Logger, d.Panics), cors(d.CORSOrigins), last}
		}

		rest := e.Group("/api", trace, requestLog(d.Logger, d.Panics), cors(d.CORSOrigins))
		authed := rest.Group("", requireAuth(d.Lobby), limitAccount(d.Limit))
		for _, add := range modules {
			add(REST{Public: rest, Authed: authed})
		}

		e.NoRoute(edge(func(c *gin.Context) { WritePublic(c, model.NoSuchEndpoint) })...)
		e.NoMethod(edge(func(c *gin.Context) { WritePublic(c, model.MethodNotAllowed) })...)
	}
}

var errMalformed = errors.New("api: malformed request body")

// Decode reads one JSON document into v within bodyReadTimeout, refusing trailing data and unknown fields.
func Decode(c *gin.Context, v any) error {
	// net/http lifts the deadline when the body ends; a recorder in tests has no connection to set it on.
	_ = http.NewResponseController(c.Writer).SetReadDeadline(time.Now().Add(bodyReadTimeout))
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			WritePublic(c, model.BodyTooLarge)
			return err
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			WritePublic(c, model.BodyTimeout)
			return err
		}
		WritePublic(c, model.MalformedBody)
		return err
	}
	if dec.More() {
		WritePublic(c, model.MalformedBody)
		return errMalformed
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		WritePublic(c, model.MalformedBody)
		return errMalformed
	}
	return nil
}

// Missing rejects a request with a required field left out; validating values is the module's job.
func Missing(c *gin.Context, fields ...string) bool {
	if slices.Contains(fields, "") {
		WritePublic(c, model.MissingField)
		return true
	}
	return false
}
