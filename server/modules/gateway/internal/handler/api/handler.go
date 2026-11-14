// Package api serves the gateway's REST API on Gin, one Lobby use case per endpoint.
package api

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

	"finga/internal/kernel"
	"finga/modules/gateway/internal/application"
	"finga/modules/gateway/internal/model"
	"finga/pkg/panics"
)

const (
	maxBodyBytes    = 64 << 10         // the largest legitimate body holds a 128-character password
	bodyReadTimeout = 10 * time.Second // from the end of the headers, which the server's own timeout covers
)

// Dependencies are the use cases and settings the handler is built from.
type Dependencies struct {
	Lobby       *application.Lobby
	Logger      *slog.Logger
	CORSOrigins []string
	// Panics records a panic in a request.
	Panics *panics.Recorder
	// Traces starts each request's trace.
	Traces trace.TracerProvider
}

type api struct {
	lobby *application.Lobby
}

// Mount serves the REST API under /api; unrouted paths also pass the log and CORS, so any preflight is answered.
func Mount(d Dependencies) func(e *gin.Engine) {
	return func(e *gin.Engine) {
		h := &api{lobby: d.Lobby}
		trace := traced(d.Traces)
		edge := func(last gin.HandlerFunc) []gin.HandlerFunc {
			return []gin.HandlerFunc{trace, requestLog(d.Logger, d.Panics), cors(d.CORSOrigins), last}
		}

		rest := e.Group("/api", trace, requestLog(d.Logger, d.Panics), cors(d.CORSOrigins))
		rest.POST("/accounts", h.register)
		rest.POST("/sessions", h.login)

		authed := rest.Group("", requireAuth(d.Lobby))
		authed.DELETE("/sessions/current", h.logout)
		authed.DELETE("/sessions", h.logoutEverywhere)
		authed.GET("/characters", h.listCharacters)
		authed.POST("/characters", h.createCharacter)
		authed.PUT("/characters/:id/skill-bar", h.setSkillBar)
		authed.GET("/settings", h.settings)
		authed.PUT("/settings", h.updateSettings)

		e.NoRoute(edge(func(c *gin.Context) { writePublic(c, model.NoSuchEndpoint) })...)
		e.NoMethod(edge(func(c *gin.Context) { writePublic(c, model.MethodNotAllowed) })...)
	}
}

var errMalformed = errors.New("api: malformed request body")

// decode reads one JSON document into v within bodyReadTimeout, refusing trailing data and unknown fields.
func decode(c *gin.Context, v any) error {
	// net/http lifts the deadline when the body ends; a recorder in tests has no connection to set it on.
	_ = http.NewResponseController(c.Writer).SetReadDeadline(time.Now().Add(bodyReadTimeout))
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writePublic(c, model.BodyTooLarge)
			return err
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			writePublic(c, model.BodyTimeout)
			return err
		}
		writePublic(c, model.MalformedBody)
		return err
	}
	if dec.More() {
		writePublic(c, model.MalformedBody)
		return errMalformed
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writePublic(c, model.MalformedBody)
		return errMalformed
	}
	return nil
}

// missing rejects a request with a required field left out; validating values is the module's job.
func missing(c *gin.Context, fields ...string) bool {
	if slices.Contains(fields, "") {
		writePublic(c, model.MissingField)
		return true
	}
	return false
}

func (h *api) register(c *gin.Context) {
	var req RegisterRequest
	if decode(c, &req) != nil || missing(c, req.Username, req.Password) {
		return
	}
	acc, err := h.lobby.Register(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		writeError(c, err)
		return
	}
	writeJSON(c, http.StatusCreated, toAccount(acc))
}

func (h *api) login(c *gin.Context) {
	var req LoginRequest
	if decode(c, &req) != nil || missing(c, req.Username, req.Password) {
		return
	}
	token, err := h.lobby.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		writeError(c, err)
		return
	}
	writeJSON(c, http.StatusCreated, SessionResponse{Token: token})
}

func (h *api) logout(c *gin.Context) {
	if err := h.lobby.Logout(c.Request.Context(), principalFrom(c)); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *api) logoutEverywhere(c *gin.Context) {
	if err := h.lobby.LogoutEverywhere(c.Request.Context(), principalFrom(c)); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *api) listCharacters(c *gin.Context) {
	cs, err := h.lobby.Characters(c.Request.Context(), principalFrom(c))
	if err != nil {
		writeError(c, err)
		return
	}
	writeJSON(c, http.StatusOK, toCharacters(cs))
}

func (h *api) createCharacter(c *gin.Context) {
	var req CreateCharacterRequest
	if decode(c, &req) != nil || missing(c, req.Name) {
		return
	}
	ch, err := h.lobby.CreateCharacter(c.Request.Context(), principalFrom(c), req.Name)
	if err != nil {
		writeError(c, err)
		return
	}
	writeJSON(c, http.StatusCreated, toCharacter(ch))
}

func (h *api) setSkillBar(c *gin.Context) {
	id, err := kernel.ParseCharacterID(c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	var req SkillBarRequest
	if decode(c, &req) != nil {
		return
	}
	if err := h.lobby.SetSkillBar(c.Request.Context(), principalFrom(c), id, req.Slots); err != nil {
		writeError(c, err)
		return
	}
	writeJSON(c, http.StatusOK, SkillBarResponse(req))
}

func (h *api) settings(c *gin.Context) {
	s, err := h.lobby.Settings(c.Request.Context(), principalFrom(c))
	if err != nil {
		writeError(c, err)
		return
	}
	writeJSON(c, http.StatusOK, toSettings(s))
}

func (h *api) updateSettings(c *gin.Context) {
	var req SettingsRequest
	if decode(c, &req) != nil || missing(c, req.ControlPreset) {
		return
	}
	s := model.Settings{ControlPreset: req.ControlPreset, AutoAssignPoints: req.AutoAssignPoints, ChatPinned: req.ChatPinned}
	if err := h.lobby.UpdateSettings(c.Request.Context(), principalFrom(c), s); err != nil {
		writeError(c, err)
		return
	}
	writeJSON(c, http.StatusOK, toSettings(s))
}
