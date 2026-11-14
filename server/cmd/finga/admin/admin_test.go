package admin_test

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

	"finga/cmd/finga/admin"
	"finga/internal/adminapi"
	"finga/internal/kernel"
	"finga/modules/account"
	"finga/modules/character"
	"finga/modules/item"
	"finga/pkg/transient"
)

// modules fail where a test says.
type modules struct{ validate, load, inventory error }

func (m modules) Validate(context.Context, string) (account.Principal, error) {
	return account.Principal{AccountID: kernel.NewAccountID()}, m.validate
}

func (m modules) Load(context.Context, kernel.CharacterID, kernel.AccountID) (character.Loaded, error) {
	return character.Loaded{}, m.load
}

func (modules) SaveCheckpoint(context.Context, character.Checkpoint) error { return nil }

func (m modules) Inventory(context.Context, kernel.CharacterID) (item.Inventory, error) {
	return item.Inventory{}, m.inventory
}

func (modules) Add(context.Context, item.Actor, item.AddRequest) (item.Result, error) {
	return item.Result{}, nil
}

func call(t *testing.T, m modules, method, path, body string) (int, string, string) {
	t.Helper()
	var logs bytes.Buffer
	h := admin.Handler(admin.Deps{Log: slog.New(slog.NewTextHandler(&logs, nil)), Accounts: m, Characters: m, Items: m})
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
		m      modules
		status int
		reason string
		logged bool
	}{
		"a module down":   {modules{load: fmt.Errorf("%w: no database", transient.ErrUnavailable)}, http.StatusServiceUnavailable, "unavailable", false},
		"a caller gone":   {modules{inventory: context.Canceled}, http.StatusServiceUnavailable, "unavailable", false},
		"a busy module":   {modules{validate: kernel.ErrBusy}, http.StatusServiceUnavailable, "busy", false},
		"a module's bug":  {modules{inventory: fmt.Errorf("%w: a panic", kernel.ErrInternal)}, http.StatusInternalServerError, "internal", true},
		"a plain failure": {modules{inventory: errors.New("boom")}, http.StatusInternalServerError, "internal", true},
	} {
		status, reason, logs := call(t, c.m, http.MethodGet, items, "")
		if status != c.status || reason != c.reason || (logs != "") != c.logged {
			t.Errorf("%s: %d %q, logged %q; want %d %q, logged %v", name, status, reason, logs, c.status, c.reason, c.logged)
		}
	}
}

// A body the path doesn't take is refused before anything is written.
func TestABadBodyIsRefused(t *testing.T) {
	progress := "/admin/characters/" + kernel.NewCharacterID().String() + "/progress"
	for _, body := range []string{`{"level": 5, "gold": 1}`, `{"level": "five"}`, ``} {
		if status, reason, _ := call(t, modules{}, http.MethodPut, progress, body); status != http.StatusBadRequest || reason != "bad_body" {
			t.Errorf("%q: %d %q; want 400 bad_body", body, status, reason)
		}
	}
}
