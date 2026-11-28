package transport_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"finga/internal/application/character"
	"finga/internal/application/item"
	hcharacter "finga/internal/handler/character"
	"finga/internal/handler/transport"
	hworld "finga/internal/handler/world"
	"finga/internal/shared/adminapi"
	"finga/internal/shared/kernel"
	"finga/pkg/transient"
)

// modules fail where a test says.
type adminModules struct{ validate, load, inventory error }

func (m adminModules) Validate(context.Context, string) (kernel.AccountID, error) {
	return kernel.NewAccountID(), m.validate
}

func (m adminModules) Load(context.Context, kernel.CharacterID, kernel.AccountID) (character.Loaded, error) {
	return character.Loaded{}, m.load
}

func (adminModules) SaveCheckpoint(context.Context, character.Checkpoint) error { return nil }

func (adminModules) FindWaypoints(context.Context, kernel.CharacterID, []character.Waypoint) error {
	return nil
}

func (adminModules) Offline(context.Context, kernel.CharacterID, func(context.Context) error) (bool, error) {
	return true, nil
}

func (m adminModules) Inventory(context.Context, kernel.CharacterID) (item.Inventory, error) {
	return item.Inventory{}, m.inventory
}

func (adminModules) Add(context.Context, item.Actor, item.AddRequest) (item.Result, error) {
	return item.Result{}, nil
}

func adminCall(t *testing.T, m adminModules, method, path, body string) (int, string, string) {
	t.Helper()
	var logs bytes.Buffer
	h := transport.NewAdmin(transport.AdminOptions{Validate: m.Validate, Log: slog.New(slog.NewTextHandler(&logs, nil))},
		hcharacter.AdminRoutes(hcharacter.AdminDeps{Characters: m, Items: m}),
		hworld.AdminRoutes(hworld.AdminDeps{Characters: m, World: m}),
	)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var e adminapi.Error
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	return rec.Code, e.Reason, logs.String()
}

// A failure the caller can't fix answers 503 or 500; only a failure inside the game is logged.
func TestAFailureAnswersItsStatus(t *testing.T) {
	items := "/admin/characters/" + kernel.NewCharacterID().String() + "/items"
	for name, c := range map[string]struct {
		m      adminModules
		status int
		reason string
		logged bool
	}{
		"a module down":   {adminModules{load: fmt.Errorf("%w: no database", transient.ErrUnavailable)}, http.StatusServiceUnavailable, "unavailable", false},
		"a caller gone":   {adminModules{inventory: context.Canceled}, http.StatusServiceUnavailable, "unavailable", false},
		"a busy module":   {adminModules{validate: kernel.ErrBusy}, http.StatusServiceUnavailable, "busy", false},
		"a module's bug":  {adminModules{inventory: fmt.Errorf("%w: a panic", kernel.ErrInternal)}, http.StatusInternalServerError, "internal", true},
		"a plain failure": {adminModules{inventory: errors.New("boom")}, http.StatusInternalServerError, "internal", true},
	} {
		status, reason, logs := adminCall(t, c.m, http.MethodGet, items, "")
		if status != c.status || reason != c.reason || (logs != "") != c.logged {
			t.Errorf("%s: %d %q, logged %q; want %d %q, logged %v", name, status, reason, logs, c.status, c.reason, c.logged)
		}
	}
}

// A body the path doesn't take is refused before anything is written.
func TestABadBodyIsRefused(t *testing.T) {
	progress := "/admin/characters/" + kernel.NewCharacterID().String() + "/progress"
	for _, body := range []string{`{"level": 5, "gold": 1}`, `{"level": "five"}`, ``} {
		if status, reason, _ := adminCall(t, adminModules{}, http.MethodPut, progress, body); status != http.StatusBadRequest || reason != "bad_body" {
			t.Errorf("%q: %d %q; want 400 bad_body", body, status, reason)
		}
	}
}

// Every admin route is behind the bearer check, which runs before the path is read: no token, or one the account module refuses, never reaches a route.
func TestAnAdminRouteWithoutAValidTokenIsUnauthenticated(t *testing.T) {
	var logs bytes.Buffer
	m := adminModules{validate: kernel.ErrUnauthenticated}
	h := transport.NewAdmin(transport.AdminOptions{Validate: m.Validate, Log: slog.New(slog.NewTextHandler(&logs, nil))},
		hcharacter.AdminRoutes(hcharacter.AdminDeps{Characters: m, Items: m}),
		hworld.AdminRoutes(hworld.AdminDeps{Characters: m, World: m}),
	)
	for name, auth := range map[string]string{"no header": "", "not a bearer": "Basic abc", "a refused token": "Bearer nope"} {
		req := httptest.NewRequest(http.MethodGet, "/admin/characters/not-an-id", nil) // a path no route could parse: the check must come first
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: %d, want 401", name, rec.Code)
		}
	}
}
