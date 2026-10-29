// Package api is the REST handler layer (Low-level design, pages 4 and
// 11): it decodes requests, resolves the caller from the bearer token,
// calls exactly one of the application's Lobby use cases per endpoint, and
// maps results and errors to HTTP. Business rules live in the services;
// this package only rejects input that is malformed whatever the rules are.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"

	"finga/services/gateway/internal/application"
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

type handler struct {
	lobby *application.Lobby
}

// NewHandler builds the /api routes with their middleware: logging and
// panic recovery outermost, then CORS, then authentication where needed.
func NewHandler(d Dependencies) http.Handler {
	h := &handler{lobby: d.Lobby}
	mux := http.NewServeMux()
	public := func(fn http.HandlerFunc) http.Handler { return fn }
	authed := func(fn http.HandlerFunc) http.Handler { return requireAuth(d.Lobby, fn) }

	mux.Handle("POST /api/accounts", public(h.register))
	mux.Handle("POST /api/sessions", public(h.login))
	mux.Handle("DELETE /api/sessions/current", authed(h.logout))
	mux.Handle("DELETE /api/sessions", authed(h.logoutEverywhere))
	mux.Handle("GET /api/characters", authed(h.listCharacters))
	mux.Handle("POST /api/characters", authed(h.createCharacter))
	mux.Handle("GET /api/settings", authed(h.settings))
	mux.Handle("PUT /api/settings", authed(h.updateSettings))
	mux.HandleFunc("/", notFound)

	return withLogging(d.Logger, withCORS(d.CORSOrigins, mux))
}

// notFound answers unknown paths and wrong methods with a JSON error, so
// clients never see the mux's plain-text defaults.
func notFound(w http.ResponseWriter, r *http.Request) {
	for _, route := range Routes() {
		if route.Path == r.URL.Path {
			writePublic(w, model.MethodNotAllowed)
			return
		}
	}
	writePublic(w, model.NoSuchEndpoint)
}

// errMalformed tells decode's caller the request was refused.
var errMalformed = errors.New("api: malformed request body")

// decode reads one JSON document into v, rejecting unknown fields, trailing
// data, and bodies over the limit.
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writePublic(w, model.BodyTooLarge)
			return err
		}
		writePublic(w, model.MalformedBody)
		return err
	}
	if dec.More() {
		writePublic(w, model.MalformedBody)
		return errMalformed
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writePublic(w, model.MalformedBody)
		return errMalformed
	}
	return nil
}

// missing rejects a structurally incomplete request: a required field left
// out. Whether the value is acceptable is the service's rule.
func missing(w http.ResponseWriter, fields ...string) bool {
	for _, f := range fields {
		if f == "" {
			writePublic(w, model.MissingField)
			return true
		}
	}
	return false
}

func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if decode(w, r, &req) != nil || missing(w, req.Username, req.Password) {
		return
	}
	acc, err := h.lobby.Register(r.Context(), req.Username, req.Password)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAccount(acc))
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if decode(w, r, &req) != nil || missing(w, req.Username, req.Password) {
		return
	}
	token, err := h.lobby.Login(r.Context(), req.Username, req.Password, clientIP(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, SessionResponse{Token: token})
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.lobby.Logout(r.Context(), principalFrom(r.Context())); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) listCharacters(w http.ResponseWriter, r *http.Request) {
	cs, err := h.lobby.Characters(r.Context(), principalFrom(r.Context()))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toCharacters(cs))
}

func (h *handler) createCharacter(w http.ResponseWriter, r *http.Request) {
	var req CreateCharacterRequest
	if decode(w, r, &req) != nil || missing(w, req.Name, req.Class) {
		return
	}
	c, err := h.lobby.CreateCharacter(r.Context(), principalFrom(r.Context()), req.Name, req.Class)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toCharacter(c))
}

func (h *handler) settings(w http.ResponseWriter, r *http.Request) {
	s, err := h.lobby.Settings(r.Context(), principalFrom(r.Context()))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettings(s))
}

func (h *handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var req SettingsRequest
	if decode(w, r, &req) != nil || missing(w, req.ControlPreset) {
		return
	}
	s := model.Settings{ControlPreset: req.ControlPreset}
	if err := h.lobby.UpdateSettings(r.Context(), principalFrom(r.Context()), s); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettings(s))
}

// clientIP is the peer's address. Under make up the browser's requests come
// through nginx, which sets X-Forwarded-For, but the gateway does not trust
// the header yet (Stage 2, with the edge's TLS and limits), so every browser
// behind nginx shares its address for the login throttle.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// logoutEverywhere ends every session of the account; the account service
// tells the gateway, which closes the account's connections.
func (h *handler) logoutEverywhere(w http.ResponseWriter, r *http.Request) {
	if err := h.lobby.LogoutEverywhere(r.Context(), principalFrom(r.Context())); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
