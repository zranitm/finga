// Package api serves the gateway's REST API on Gin, one Lobby use case per endpoint.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"finga/pkg/panics"
	"finga/services/gateway/internal/application"
	"finga/services/gateway/internal/handler"
	"finga/services/gateway/internal/model"
)

// maxBodyBytes caps a request body; the largest legitimate one holds a 128-character password.
const maxBodyBytes = 64 << 10

// Dependencies are the use cases and settings the handler is built from.
type Dependencies struct {
	Lobby       *application.Lobby
	Logger      *slog.Logger
	CORSOrigins []string
	// Panics records a panic in a request; nil logs it to Logger.
	Panics *panics.Recorder
}

// Route is one endpoint, for the OpenAPI drift check.
type Route struct {
	Method string
	Path   string
}

// Routes lists every endpoint the handler serves.
func Routes() []Route {
	return []Route{
		{http.MethodPost, "/api/accounts"},
		{http.MethodPost, "/api/sessions"},
		{http.MethodDelete, "/api/sessions/current"},
		{http.MethodDelete, "/api/sessions"},
		{http.MethodGet, "/api/characters"},
		{http.MethodPost, "/api/characters"},
		{http.MethodGet, "/api/settings"},
		{http.MethodPut, "/api/settings"},
	}
}

type api struct {
	lobby *application.Lobby
}

// Mount serves the REST API under /api; unrouted paths also pass the log and CORS, so any preflight is answered.
func Mount(d Dependencies) func(e *gin.Engine) {
	if d.Panics == nil {
		d.Panics = panics.NewRecorder(d.Logger, nil)
	}
	return func(e *gin.Engine) {
		h := &api{lobby: d.Lobby}
		edge := func(last gin.HandlerFunc) []gin.HandlerFunc {
			return []gin.HandlerFunc{requestLog(d.Logger, d.Panics), cors(d.CORSOrigins), last}
		}

		rest := e.Group("/api", requestLog(d.Logger, d.Panics), cors(d.CORSOrigins))
		rest.POST("/accounts", h.register)
		rest.POST("/sessions", h.login)

		authed := rest.Group("", requireAuth(d.Lobby))
		authed.DELETE("/sessions/current", h.logout)
		authed.DELETE("/sessions", h.logoutEverywhere)
		authed.GET("/characters", h.listCharacters)
		authed.POST("/characters", h.createCharacter)
		authed.GET("/settings", h.settings)
		authed.PUT("/settings", h.updateSettings)

		e.NoRoute(edge(func(c *gin.Context) { writePublic(c, model.NoSuchEndpoint) })...)
		e.NoMethod(edge(func(c *gin.Context) { writePublic(c, model.MethodNotAllowed) })...)
	}
}

// NewHandler serves the REST API alone on its own engine, for tests.
func NewHandler(d Dependencies) http.Handler {
	e := handler.NewEngine()
	Mount(d)(e)
	return e
}

var errMalformed = errors.New("api: malformed request body")

// decode reads one JSON document into v; unlike Gin's binding it refuses trailing data and unknown fields.
func decode(c *gin.Context, v any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			writePublic(c, model.BodyTooLarge)
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

// missing rejects a request with a required field left out; validating values is the service's job.
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
	if decode(c, &req) != nil || missing(c, req.Name, req.Class) {
		return
	}
	ch, err := h.lobby.CreateCharacter(c.Request.Context(), principalFrom(c), req.Name, req.Class)
	if err != nil {
		writeError(c, err)
		return
	}
	writeJSON(c, http.StatusCreated, toCharacter(ch))
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
	s := model.Settings{ControlPreset: req.ControlPreset}
	if err := h.lobby.UpdateSettings(c.Request.Context(), principalFrom(c), s); err != nil {
		writeError(c, err)
		return
	}
	writeJSON(c, http.StatusOK, toSettings(s))
}
