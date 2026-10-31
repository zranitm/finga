// Package api is the REST handler layer (Low-level design, pages 4 and
// 11) on Gin (TD-26): it decodes requests, resolves the caller from the
// bearer token, calls exactly one of the application's Lobby use cases per
// endpoint, and maps results and errors to HTTP. Business rules live in
// the services; this package only rejects input that is malformed whatever
// the rules are.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"

	"github.com/gin-gonic/gin"

	"finga/services/gateway/internal/application"
	"finga/services/gateway/internal/handler"
	"finga/services/gateway/internal/model"
)

// maxBodyBytes caps a request body; the biggest legitimate body is a
// password of 128 characters.
const maxBodyBytes = 64 << 10

// Dependencies are the use cases and settings the handler is built from.
type Dependencies struct {
	Lobby       *application.Lobby
	Logger      *slog.Logger
	CORSOrigins []string
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

// Mount puts the REST API on e under /api: the request log with panic
// recovery outermost, then CORS, then authentication on the routes that
// need a session. A path the engine does not route gets the API's JSON
// answer (no_such_endpoint, or method_not_allowed for a known path asked
// with another method) through the same log and CORS, so a preflight for
// any path is answered too.
func Mount(d Dependencies) func(e *gin.Engine) {
	return func(e *gin.Engine) {
		h := &api{lobby: d.Lobby}
		edge := func(last gin.HandlerFunc) []gin.HandlerFunc {
			return []gin.HandlerFunc{requestLog(d.Logger), cors(d.CORSOrigins), last}
		}

		rest := e.Group("/api", requestLog(d.Logger), cors(d.CORSOrigins))
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

// NewHandler is the REST API alone, on an engine of its own as the gateway
// builds it: what the handler's tests serve.
func NewHandler(d Dependencies) http.Handler {
	e := handler.NewEngine()
	Mount(d)(e)
	return e
}

// errMalformed tells decode's caller the request was refused.
var errMalformed = errors.New("api: malformed request body")

// decode reads one JSON document into v, rejecting unknown fields, trailing
// data, and bodies over the limit. It is stricter than Gin's binding, which
// accepts trailing data and switches unknown-field checks on for every
// engine at once.
func decode(c *gin.Context, v any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
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

// missing rejects a structurally incomplete request: a required field left
// out. Whether the value is acceptable is the service's rule.
func missing(c *gin.Context, fields ...string) bool {
	for _, f := range fields {
		if f == "" {
			writePublic(c, model.MissingField)
			return true
		}
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
	token, err := h.lobby.Login(c.Request.Context(), req.Username, req.Password, clientIP(c.Request))
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

// logoutEverywhere ends every session of the account; the account service
// tells the gateway, which closes the account's connections.
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

// clientIP is the peer's address. Under make up the browser's requests come
// through nginx, which sets X-Forwarded-For, but the gateway does not trust
// the header yet (Stage 2, with the edge's TLS and limits), so every browser
// behind nginx shares its address for the login throttle. Gin's ClientIP
// is not used, and the engine trusts no proxy (handler.NewEngine).
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
