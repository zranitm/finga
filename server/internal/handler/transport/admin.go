package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"finga/internal/shared/adminapi"
	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// adminMaxBody bounds an admin request's body; the largest, a character's progress, is a few kilobytes.
const adminMaxBody = 64 << 10

// The failures only the admin API has, answered with their reason.
var (
	ErrAdminBadBody     = errors.New("bad_body")         // a body that isn't the path's JSON
	ErrAdminBadProgress = errors.New("bad_progress")     // progress the game data doesn't allow
	ErrAdminOnline      = errors.New("character_online") // the world holds the character, and would overwrite the write
)

// AdminOptions are what the admin API checks every request with: Validate resolves a session's bearer token to its account.
type AdminOptions struct {
	Validate func(ctx context.Context, token string) (kernel.AccountID, error)
	Log      *slog.Logger
}

// Admin is the private admin API (internal/shared/adminapi): every route behind a bearer token, each module's handler adding its own.
type Admin struct {
	mux  *http.ServeMux
	opts AdminOptions
}

// AdminRoutes adds a module's admin routes to an Admin.
type AdminRoutes func(*Admin)

type adminAccountKey struct{}

// NewAdmin is the admin API over the routes the modules' handlers add; the wiring builds it only where the admin switch is on.
func NewAdmin(opts AdminOptions, routes ...AdminRoutes) *Admin {
	a := &Admin{mux: http.NewServeMux(), opts: opts}
	for _, add := range routes {
		add(a)
	}
	return a
}

// ServeHTTP serves the admin API.
func (a *Admin) ServeHTTP(w http.ResponseWriter, r *http.Request) { a.mux.ServeHTTP(w, r) }

// Handle adds a route, an adminapi path with its method, behind the bearer check: a request without a valid token never reaches h, and the account its token names is AdminAccount's.
func (a *Admin) Handle(pattern string, h http.HandlerFunc) {
	a.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok {
			a.Fail(w, r, kernel.ErrUnauthenticated)
			return
		}
		account, err := a.opts.Validate(r.Context(), token)
		if err != nil {
			a.Fail(w, r, err)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), adminAccountKey{}, account)))
	})
}

// AdminAccount is the account the request's bearer token named; it is set on every route Handle adds.
func AdminAccount(r *http.Request) kernel.AccountID {
	id, _ := r.Context().Value(adminAccountKey{}).(kernel.AccountID)
	return id
}

// AdminOwned is the character the path names as its account's, the bearer token's, loaded by load; any other is not found.
func AdminOwned[L any](r *http.Request, load func(ctx context.Context, id kernel.CharacterID, account kernel.AccountID) (L, error)) (kernel.CharacterID, kernel.AccountID, L, error) {
	var (
		none      L
		noAccount kernel.AccountID
	)
	id, err := kernel.ParseCharacterID(r.PathValue("id"))
	if err != nil {
		return id, noAccount, none, err
	}
	account := AdminAccount(r)
	loaded, err := load(r.Context(), id, account)
	return id, account, loaded, err
}

// Fail answers err with its status and reason: a rule the caller broke, or internal, logged.
func (a *Admin) Fail(w http.ResponseWriter, r *http.Request, err error) {
	status, detail := http.StatusInternalServerError, ""
	reason, ok := kernel.ReasonOf(err)
	switch {
	case errors.Is(err, ErrAdminBadBody):
		status, reason, detail = http.StatusBadRequest, ErrAdminBadBody.Error(), err.Error()
	case errors.Is(err, ErrAdminBadProgress):
		status, reason, detail = http.StatusUnprocessableEntity, ErrAdminBadProgress.Error(), err.Error()
	case errors.Is(err, ErrAdminOnline):
		status, reason = http.StatusConflict, ErrAdminOnline.Error()
	case errors.Is(err, kernel.ErrUnauthenticated):
		status = http.StatusUnauthorized
	case errors.Is(err, kernel.ErrCharacterNotFound):
		status = http.StatusNotFound
	case errors.Is(err, kernel.ErrBadID):
		status = http.StatusBadRequest
	case kernel.IsRule(err):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, transient.ErrUnavailable), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		status = http.StatusServiceUnavailable
		if !ok {
			reason = "unavailable"
		}
	default:
		reason = "internal"
		a.opts.Log.ErrorContext(r.Context(), "admin call failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	AdminWrite(w, status, adminapi.Error{Reason: reason, Detail: detail})
}

// AdminDecode reads one JSON document into v, refusing a field the path doesn't take.
func AdminDecode(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, adminMaxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %w", ErrAdminBadBody, err)
	}
	return nil
}

// AdminWrite answers status with body as JSON.
func AdminWrite(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body) // the caller gone, there's no one to tell
}
