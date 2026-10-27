// Package api is the REST handler layer (Low-level design, page 4): it
// decodes requests, resolves the caller from the bearer token, calls
// exactly one service method per endpoint, and maps results and errors to
// HTTP. Business rules live in the services; this package only rejects
// input that is malformed whatever the rules are.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"

	"google.golang.org/grpc"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/application/character"
	"finga/internal/handler/errmap"
	"finga/internal/model"
	"finga/pkg/auth"
)

// maxBodyBytes caps a request body; the biggest legitimate body is a
// password of 128 characters.
const maxBodyBytes = 64 << 10

// AccountService is what the handler needs from the account service
// (page 10); its generated client satisfies it.
type AccountService interface {
	Register(ctx context.Context, in *accountv1.RegisterRequest, opts ...grpc.CallOption) (*accountv1.RegisterResponse, error)
	Login(ctx context.Context, in *accountv1.LoginRequest, opts ...grpc.CallOption) (*accountv1.LoginResponse, error)
	GetSettings(ctx context.Context, in *accountv1.GetSettingsRequest, opts ...grpc.CallOption) (*accountv1.GetSettingsResponse, error)
	UpdateSettings(ctx context.Context, in *accountv1.UpdateSettingsRequest, opts ...grpc.CallOption) (*accountv1.UpdateSettingsResponse, error)
}

// CharacterService is what the handler needs from the character module.
type CharacterService interface {
	Create(ctx context.Context, owner model.AccountID, in character.CreateInput) (character.Character, error)
	List(ctx context.Context, owner model.AccountID) ([]character.Character, error)
}

// Dependencies are the services and settings the handler is built from.
type Dependencies struct {
	Accounts    AccountService
	Characters  CharacterService
	Sessions    auth.Guard
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
	accounts   AccountService
	characters CharacterService
	sessions   auth.Guard
}

// NewHandler builds the /api routes with their middleware: logging and
// panic recovery outermost, then CORS, then authentication where needed.
func NewHandler(d Dependencies) http.Handler {
	h := &handler{accounts: d.Accounts, characters: d.Characters, sessions: d.Sessions}
	mux := http.NewServeMux()
	public := func(fn http.HandlerFunc) http.Handler { return fn }
	authed := func(fn http.HandlerFunc) http.Handler { return requireAuth(d.Sessions, fn) }

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
			writePublic(w, errmap.MethodNotAllowed)
			return
		}
	}
	writePublic(w, errmap.NoSuchEndpoint)
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
			writePublic(w, errmap.BodyTooLarge)
			return err
		}
		writePublic(w, errmap.MalformedBody)
		return err
	}
	if dec.More() {
		writePublic(w, errmap.MalformedBody)
		return errMalformed
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		writePublic(w, errmap.MalformedBody)
		return errMalformed
	}
	return nil
}

// missing rejects a structurally incomplete request: a required field left
// out. Whether the value is acceptable is the service's rule.
func missing(w http.ResponseWriter, fields ...string) bool {
	for _, f := range fields {
		if f == "" {
			writePublic(w, errmap.MissingField)
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
	res, err := h.accounts.Register(r.Context(), &accountv1.RegisterRequest{Username: req.Username, Password: req.Password})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toAccount(res.GetAccount()))
}

func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if decode(w, r, &req) != nil || missing(w, req.Username, req.Password) {
		return
	}
	res, err := h.accounts.Login(r.Context(), &accountv1.LoginRequest{Username: req.Username, Password: req.Password, Ip: clientIP(r)})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, SessionResponse{Token: res.GetToken()})
}

func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.sessions.Revoke(r.Context(), principalFrom(r.Context()).SessionID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) listCharacters(w http.ResponseWriter, r *http.Request) {
	cs, err := h.characters.List(r.Context(), model.AccountID(principalFrom(r.Context()).AccountID))
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
	c, err := h.characters.Create(r.Context(), model.AccountID(principalFrom(r.Context()).AccountID), character.CreateInput{Name: req.Name, Class: model.Class(req.Class)})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toCharacter(c))
}

func (h *handler) settings(w http.ResponseWriter, r *http.Request) {
	res, err := h.accounts.GetSettings(r.Context(), &accountv1.GetSettingsRequest{AccountId: principalFrom(r.Context()).AccountID.String()})
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettings(res.GetSettings()))
}

func (h *handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	var req SettingsRequest
	if decode(w, r, &req) != nil || missing(w, req.ControlPreset) {
		return
	}
	s := &accountv1.Settings{ControlPreset: req.ControlPreset}
	if _, err := h.accounts.UpdateSettings(r.Context(), &accountv1.UpdateSettingsRequest{AccountId: principalFrom(r.Context()).AccountID.String(), Settings: s}); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettings(s))
}

// clientIP is the peer's address. Stage 1 has no proxy in front, so the
// socket address is the client; Stage 2's nginx will set X-Forwarded-For.
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
	if err := h.sessions.RevokeAll(r.Context(), principalFrom(r.Context()).AccountID); err != nil {
		writeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
