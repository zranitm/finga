package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"finga/internal/gamedata"
	"finga/internal/kernel"
	"finga/modules/account"
	"finga/modules/character"
	"finga/modules/gateway/internal/application"
	"finga/modules/gateway/internal/handler"
	"finga/modules/gateway/internal/handler/api"
	"finga/modules/gateway/internal/local"
	"finga/modules/gateway/internal/model"
	"finga/pkg/observability/tracing"
	"finga/pkg/panics"
	"finga/pkg/transient"
)

const goodToken = "tok-good-0123456789abcdef"

var (
	accountID   = kernel.NewAccountID()
	sessionID   = kernel.NewSessionID()
	principal   = account.Principal{AccountID: accountID, SessionID: sessionID}
	t0          = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	sampleChar  = character.Character{ID: kernel.NewCharacterID(), AccountID: accountID, Name: "Ayla", Class: "warrior", Level: 1, OwnStats: gamedata.Stats{STR: 12, DEX: 5, INT: 4, AGI: 5, VIT: 10}, HP: 204, MaxHP: 204, MapID: "town", X: 416, Y: 608, UpdatedAt: t0, SkillBar: []string{"strike", "", "", "", "", "spark"}}
	errInternal = errors.New("pq: connection reset") // a facade passes an error its table lacks as it is
)

// traces samples as the gateway does, and spans records every span it samples.
var traces, spans = gatewayTraces()

func gatewayTraces() (*tracing.Provider, *tracetest.SpanRecorder) {
	tp, err := tracing.New(context.Background(), tracing.Config{}, tracing.Options{Service: "gateway", Root: api.EntryPoint})
	if err != nil {
		panic(err)
	}
	rec := tracetest.NewSpanRecorder()
	tp.RegisterSpanProcessor(rec)
	return tp, rec
}

type harness struct {
	accounts   *fakeAccounts
	characters *fakeCharacters
	logs       *bytes.Buffer
	handler    http.Handler
}

// newHarness wires the lobby over the modules as the gateway's build does, with fakes for the modules.
func newHarness(origins ...string) *harness {
	h := &harness{
		accounts:   &fakeAccounts{token: goodToken, principal: principal},
		characters: &fakeCharacters{},
		logs:       &bytes.Buffer{},
	}
	e := handler.NewEngine()
	log := slog.New(slog.NewJSONHandler(h.logs, nil))
	api.Mount(api.Dependencies{
		Lobby:  application.NewLobby(application.LobbyDeps{Accounts: local.NewAccounts(h.accounts), Characters: local.NewCharacters(h.characters), Sessions: local.NewGuard(h.accounts)}),
		Logger: log, CORSOrigins: origins, Panics: panics.NewRecorder(log, nil), Traces: traces,
	})(e)
	h.handler = e
	return h
}

func (h *harness) do(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	return rec
}

func authed() map[string]string { return map[string]string{"Authorization": "Bearer " + goodToken} }

func decode(t *testing.T, rec *httptest.ResponseRecorder, into any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
}

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, want model.Public) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	var body api.ErrorResponse
	decode(t, rec, &body)
	if body.Code != want.Code || body.Message == "" {
		t.Fatalf("error body %+v, want code %d (%s) with a message", body, want.Code, want.Name)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type %q", rec.Header().Get("Content-Type"))
	}
}

// errorRows are the service errors every endpoint must map the same way.
var errorRows = []struct {
	name   string
	err    error
	status int
	code   model.Public
}{
	{"bad input is 400 with its own code", fmt.Errorf("%w: too short", kernel.ErrBadPassword), http.StatusBadRequest, model.BadPassword},
	{"unauthenticated is 401", errUnauthenticated, http.StatusUnauthorized, model.Unauthenticated},
	{"a missing character is 404 CHARACTER_NOT_FOUND", fmt.Errorf("%w: no such row", kernel.ErrCharacterNotFound), http.StatusNotFound, model.CharacterNotFound},
	{"a taken username is 409 USERNAME_TAKEN", fmt.Errorf("%w: unique violation", kernel.ErrUsernameTaken), http.StatusConflict, model.UsernameTaken},
	{"too many logins is 429 LOGIN_RATE_LIMITED", fmt.Errorf("%w: 5 in a minute", kernel.ErrLoginRateLimited), http.StatusTooManyRequests, model.LoginRateLimited},
	{"a service that cannot be reached is 503 unavailable", fmt.Errorf("%w: connection refused", transient.ErrUnavailable), http.StatusServiceUnavailable, model.Unavailable},
	{"a wrapped error keeps its code and hides its cause", fmt.Errorf("create: %w", fmt.Errorf("%w: %w", kernel.ErrNameTaken, errInternal)), http.StatusConflict, model.NameTaken},
	{"a plain error is 500 internal without its text", errInternal, http.StatusInternalServerError, model.Internal},
}

func TestRegister(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		h := newHarness()
		var got account.Credentials
		h.accounts.register = func(_ context.Context, c account.Credentials) (account.Account, error) {
			got = c
			return account.Account{ID: accountID, Username: c.Username, CreatedAt: t0}, nil
		}
		rec := h.do("POST", "/api/accounts", `{"username":"alice","password":"correct horse"}`, nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if got.Username != "alice" || got.Password != "correct horse" {
			t.Fatalf("service got %+v", got)
		}
		var body api.AccountResponse
		decode(t, rec, &body)
		if body.ID != accountID.String() || body.Username != "alice" || !body.CreatedAt.Equal(t0) {
			t.Fatalf("body %+v", body)
		}
		if strings.Contains(h.logs.String(), "correct horse") {
			t.Fatal("the password reached a log line")
		}
	})
	t.Run("malformed input", func(t *testing.T) {
		tests := []struct {
			name string
			body string
			code model.Public
		}{
			{"not JSON", `{username`, model.MalformedBody},
			{"wrong type", `{"username":1,"password":"x"}`, model.MalformedBody},
			{"missing username", `{"password":"correct horse"}`, model.MissingField},
			{"missing password", `{"username":"alice"}`, model.MissingField},
			{"empty body", ``, model.MalformedBody},
			{"unknown field", `{"username":"alice","password":"correct horse","admin":true}`, model.MalformedBody},
			{"two JSON documents", `{"username":"alice","password":"correct horse"}{}`, model.MalformedBody},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				h := newHarness() // register is nil: a malformed request must not reach the service
				assertError(t, h.do("POST", "/api/accounts", tt.body, nil), http.StatusBadRequest, tt.code)
			})
		}
	})
	t.Run("an oversized body is 413", func(t *testing.T) {
		h := newHarness()
		big := `{"username":"alice","password":"` + strings.Repeat("p", 70*1024) + `"}`
		assertError(t, h.do("POST", "/api/accounts", big, nil), http.StatusRequestEntityTooLarge, model.BodyTooLarge)
	})
	t.Run("service errors", func(t *testing.T) {
		for _, tt := range errorRows {
			t.Run(tt.name, func(t *testing.T) {
				h := newHarness()
				h.accounts.register = func(context.Context, account.Credentials) (account.Account, error) {
					return account.Account{}, tt.err
				}
				rec := h.do("POST", "/api/accounts", `{"username":"alice","password":"correct horse"}`, nil)
				assertError(t, rec, tt.status, tt.code)
				if strings.Contains(rec.Body.String(), "connection reset") {
					t.Fatal("internal error text reached the response")
				}
			})
		}
	})
}

func TestLogin(t *testing.T) {
	t.Run("happy path passes the credentials and nothing else, and returns the token once", func(t *testing.T) {
		h := newHarness()
		var got account.Credentials
		h.accounts.login = func(_ context.Context, c account.Credentials) (string, error) {
			got = c
			return "secret-token", nil
		}
		// The client's address stays at the edge, even when a forwarding header claims one.
		rec := h.do("POST", "/api/sessions", `{"username":"alice","password":"correct horse"}`, map[string]string{"X-Forwarded-For": "203.0.113.7"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if want := (account.Credentials{Username: "alice", Password: "correct horse"}); got != want {
			t.Fatalf("service got %v, want %v", got, want)
		}
		var body api.SessionResponse
		decode(t, rec, &body)
		if body.Token != "secret-token" {
			t.Fatalf("body %+v", body)
		}
		if strings.Contains(h.logs.String(), "secret-token") {
			t.Fatal("the token reached a log line")
		}
	})
	t.Run("malformed input", func(t *testing.T) {
		h := newHarness()
		assertError(t, h.do("POST", "/api/sessions", `{"username":"alice"}`, nil), http.StatusBadRequest, model.MissingField)
	})
	t.Run("service errors", func(t *testing.T) {
		for _, tt := range errorRows {
			t.Run(tt.name, func(t *testing.T) {
				h := newHarness()
				h.accounts.login = func(context.Context, account.Credentials) (string, error) { return "", tt.err }
				assertError(t, h.do("POST", "/api/sessions", `{"username":"alice","password":"correct horse"}`, nil), tt.status, tt.code)
			})
		}
	})
}

func TestLogout(t *testing.T) {
	t.Run("revokes the current session", func(t *testing.T) {
		h := newHarness()
		rec := h.do("DELETE", "/api/sessions/current", "", authed())
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if len(h.accounts.revoked) != 1 || h.accounts.revoked[0] != sessionID {
			t.Fatalf("revoked %v", h.accounts.revoked)
		}
	})
	t.Run("needs a session", func(t *testing.T) {
		h := newHarness()
		assertError(t, h.do("DELETE", "/api/sessions/current", "", nil), http.StatusUnauthorized, model.Unauthenticated)
	})
}

func TestAuthentication(t *testing.T) {
	tests := []struct {
		name   string
		header string
	}{
		{"missing header", ""},
		{"not a bearer scheme", "Basic " + goodToken},
		{"bearer with no token", "Bearer "},
		{"unknown token", "Bearer nope"},
		{"revoked token", "Bearer revoked"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness()
			headers := map[string]string{}
			if tt.header != "" {
				headers["Authorization"] = tt.header
			}
			rec := h.do("GET", "/api/characters", "", headers)
			assertError(t, rec, http.StatusUnauthorized, model.Unauthenticated)
			if rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("WWW-Authenticate %q", rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
	t.Run("a session store failure is 500, not 401", func(t *testing.T) {
		h := newHarness()
		h.accounts.validate = func(string) (account.Principal, error) { return account.Principal{}, errInternal }
		assertError(t, h.do("GET", "/api/characters", "", authed()), http.StatusInternalServerError, model.Internal)
	})
	t.Run("bearer is matched regardless of case", func(t *testing.T) {
		h := newHarness()
		h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) {
			return nil, nil
		}
		rec := h.do("GET", "/api/characters", "", map[string]string{"Authorization": "bearer " + goodToken})
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
	})
}

func TestCharacters(t *testing.T) {
	t.Run("list for the authenticated account", func(t *testing.T) {
		h := newHarness()
		var gotOwner kernel.AccountID
		h.characters.list = func(_ context.Context, owner kernel.AccountID) ([]character.Character, error) {
			gotOwner = owner
			return []character.Character{sampleChar}, nil
		}
		rec := h.do("GET", "/api/characters", "", authed())
		if rec.Code != http.StatusOK || gotOwner != accountID {
			t.Fatalf("status %d owner %s", rec.Code, gotOwner)
		}
		var body api.CharactersResponse
		decode(t, rec, &body)
		if len(body.Characters) != 1 {
			t.Fatalf("body %+v", body)
		}
		c := body.Characters[0]
		if c.ID != sampleChar.ID.String() || c.Name != "Ayla" || c.Class != "warrior" || c.Level != 1 || c.Stats.STR != 12 || c.HP != 204 || c.MapID != "town" || !slices.Equal(c.SkillBar, sampleChar.SkillBar) {
			t.Fatalf("character %+v", c)
		}
	})
	t.Run("an empty list is an empty array, not null", func(t *testing.T) {
		h := newHarness()
		h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) {
			return nil, nil
		}
		rec := h.do("GET", "/api/characters", "", authed())
		if !strings.Contains(rec.Body.String(), `"characters":[]`) {
			t.Fatalf("body %s", rec.Body.String())
		}
	})
	t.Run("create", func(t *testing.T) {
		h := newHarness()
		var gotOwner kernel.AccountID
		var gotName string
		h.characters.create = func(_ context.Context, owner kernel.AccountID, name string) (character.Character, error) {
			gotOwner, gotName = owner, name
			return sampleChar, nil
		}
		rec := h.do("POST", "/api/characters", `{"name":"Ayla"}`, authed())
		if rec.Code != http.StatusCreated {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if gotName != "Ayla" || gotOwner != accountID {
			t.Fatalf("service got %q for %s", gotName, gotOwner)
		}
	})
	t.Run("create with malformed input", func(t *testing.T) {
		tests := []struct {
			name string
			body string
			code model.Public
		}{
			{"missing name", `{}`, model.MissingField},
			{"a class, which a new character can't choose", `{"name":"Ayla","class":"warrior"}`, model.MalformedBody},
			{"not JSON", `nope`, model.MalformedBody},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				h := newHarness()
				assertError(t, h.do("POST", "/api/characters", tt.body, authed()), http.StatusBadRequest, tt.code)
			})
		}
	})
	t.Run("save the skill bar", func(t *testing.T) {
		h := newHarness()
		var (
			gotID    kernel.CharacterID
			gotOwner kernel.AccountID
			gotBar   []string
		)
		h.characters.setBar = func(_ context.Context, id kernel.CharacterID, owner kernel.AccountID, bar []string) error {
			gotID, gotOwner, gotBar = id, owner, bar
			return nil
		}
		path := "/api/characters/" + sampleChar.ID.String() + "/skill-bar"
		rec := h.do("PUT", path, `{"slots":["","throw","","","","strike"]}`, authed())
		if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"slots":["","throw","","","","strike"]}` {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if gotID != sampleChar.ID || gotOwner != accountID || !slices.Equal(gotBar, []string{"", "throw", "", "", "", "strike"}) {
			t.Fatalf("service got %v for %s of %s", gotBar, gotID, gotOwner)
		}
		assertError(t, h.do("PUT", "/api/characters/nope/skill-bar", `{"slots":[]}`, authed()), http.StatusBadRequest, model.BadID)
		assertError(t, h.do("PUT", path, `{"slots":"strike"}`, authed()), http.StatusBadRequest, model.MalformedBody)
		h.characters.setBar = func(context.Context, kernel.CharacterID, kernel.AccountID, []string) error {
			return fmt.Errorf("%w: unknown skill fireball", kernel.ErrBadSkillBar)
		}
		assertError(t, h.do("PUT", path, `{"slots":["fireball","","","","",""]}`, authed()), http.StatusBadRequest, model.BadSkillBar)
	})
	t.Run("service errors", func(t *testing.T) {
		for _, tt := range errorRows {
			t.Run(tt.name, func(t *testing.T) {
				h := newHarness()
				h.characters.setBar = func(context.Context, kernel.CharacterID, kernel.AccountID, []string) error { return tt.err }
				assertError(t, h.do("PUT", "/api/characters/"+sampleChar.ID.String()+"/skill-bar", `{"slots":["","","","","",""]}`, authed()), tt.status, tt.code)
				h.characters.create = func(context.Context, kernel.AccountID, string) (character.Character, error) {
					return character.Character{}, tt.err
				}
				h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) { return nil, tt.err }
				assertError(t, h.do("POST", "/api/characters", `{"name":"Ayla"}`, authed()), tt.status, tt.code)
				assertError(t, h.do("GET", "/api/characters", "", authed()), tt.status, tt.code)
			})
		}
	})
}

func TestSettings(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		h := newHarness()
		h.accounts.settings = func(_ context.Context, id kernel.AccountID) (account.Settings, error) {
			if id != accountID {
				t.Fatalf("id %s", id)
			}
			return account.Settings{ControlPreset: "wasd", AutoAssignPoints: true, ChatPinned: true}, nil
		}
		rec := h.do("GET", "/api/settings", "", authed())
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		var body api.SettingsResponse
		decode(t, rec, &body)
		if body != (api.SettingsResponse{ControlPreset: "wasd", AutoAssignPoints: true, ChatPinned: true}) {
			t.Fatalf("body %+v", body)
		}
	})
	t.Run("write returns the saved settings", func(t *testing.T) {
		h := newHarness()
		var (
			savedFor kernel.AccountID
			saved    account.Settings
		)
		h.accounts.updateSettings = func(_ context.Context, id kernel.AccountID, s account.Settings) error {
			savedFor, saved = id, s
			return nil
		}
		rec := h.do("PUT", "/api/settings", `{"control_preset":"wasd","auto_assign_points":true,"chat_pinned":true}`, authed())
		if rec.Code != http.StatusOK || saved != (account.Settings{ControlPreset: "wasd", AutoAssignPoints: true, ChatPinned: true}) || savedFor != accountID {
			t.Fatalf("status %d saved %+v for %s", rec.Code, saved, savedFor)
		}
		var body api.SettingsResponse
		decode(t, rec, &body)
		if body != (api.SettingsResponse{ControlPreset: "wasd", AutoAssignPoints: true, ChatPinned: true}) {
			t.Fatalf("body %+v", body)
		}
		if rec := h.do("PUT", "/api/settings", `{"control_preset":"classic"}`, authed()); rec.Code != http.StatusOK || saved.AutoAssignPoints || saved.ChatPinned {
			t.Fatalf("status %d saved %+v; want the flags left out as false", rec.Code, saved)
		}
	})
	t.Run("write with malformed input", func(t *testing.T) {
		h := newHarness()
		assertError(t, h.do("PUT", "/api/settings", `{}`, authed()), http.StatusBadRequest, model.MissingField)
	})
	t.Run("an unknown preset is the service's rule", func(t *testing.T) {
		h := newHarness()
		h.accounts.updateSettings = func(context.Context, kernel.AccountID, account.Settings) error {
			return fmt.Errorf("%w: vim", kernel.ErrBadControlPreset)
		}
		assertError(t, h.do("PUT", "/api/settings", `{"control_preset":"vim"}`, authed()), http.StatusBadRequest, model.BadControlPreset)
	})
	t.Run("service errors", func(t *testing.T) {
		for _, tt := range errorRows {
			t.Run(tt.name, func(t *testing.T) {
				h := newHarness()
				h.accounts.settings = func(context.Context, kernel.AccountID) (account.Settings, error) { return account.Settings{}, tt.err }
				h.accounts.updateSettings = func(context.Context, kernel.AccountID, account.Settings) error { return tt.err }
				assertError(t, h.do("GET", "/api/settings", "", authed()), tt.status, tt.code)
				assertError(t, h.do("PUT", "/api/settings", `{"control_preset":"wasd"}`, authed()), tt.status, tt.code)
			})
		}
	})
}

func TestUnknownRoutes(t *testing.T) {
	h := newHarness()
	tests := []struct {
		method, path string
		status       int
	}{
		{"GET", "/api/nope", http.StatusNotFound},
		{"GET", "/api/accounts", http.StatusMethodNotAllowed},
		{"PATCH", "/api/settings", http.StatusMethodNotAllowed},
		{"POST", "/api/accounts/", http.StatusNotFound}, // Gin would redirect a trailing slash; the gateway does not
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := h.do(tt.method, tt.path, "", authed())
			if rec.Code != tt.status {
				t.Fatalf("status %d, want %d", rec.Code, tt.status)
			}
			var body api.ErrorResponse
			decode(t, rec, &body)
			if body.Code == 0 {
				t.Fatalf("no JSON error body: %s", rec.Body.String())
			}
		})
	}
}

func TestCORS(t *testing.T) {
	t.Run("with no allowed origins, no CORS headers and preflight is refused", func(t *testing.T) {
		h := newHarness()
		h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) {
			return nil, nil
		}
		rec := h.do("GET", "/api/characters", "", map[string]string{"Authorization": "Bearer " + goodToken, "Origin": "http://localhost:5173"})
		if rec.Code != http.StatusOK || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("status %d ACAO %q", rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
		}
		pre := h.do("OPTIONS", "/api/characters", "", map[string]string{"Origin": "http://localhost:5173", "Access-Control-Request-Method": "GET"})
		if pre.Code != http.StatusForbidden {
			t.Fatalf("preflight status %d", pre.Code)
		}
	})
	t.Run("with one allowed origin", func(t *testing.T) {
		h := newHarness("http://localhost:5173")
		h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) {
			return nil, nil
		}
		rec := h.do("GET", "/api/characters", "", map[string]string{"Authorization": "Bearer " + goodToken, "Origin": "http://localhost:5173"})
		if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" || !strings.Contains(rec.Header().Get("Vary"), "Origin") {
			t.Fatalf("headers %v", rec.Header())
		}
		pre := h.do("OPTIONS", "/api/characters", "", map[string]string{"Origin": "http://localhost:5173", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "authorization,content-type"})
		if pre.Code != http.StatusNoContent {
			t.Fatalf("preflight status %d", pre.Code)
		}
		for _, hdr := range []string{"Access-Control-Allow-Methods", "Access-Control-Allow-Headers", "Access-Control-Max-Age"} {
			if pre.Header().Get(hdr) == "" {
				t.Fatalf("preflight lacks %s", hdr)
			}
		}
		other := h.do("GET", "/api/characters", "", map[string]string{"Authorization": "Bearer " + goodToken, "Origin": "http://evil.example"})
		if other.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("a foreign origin got CORS headers")
		}
		if pre := h.do("OPTIONS", "/api/characters", "", map[string]string{"Origin": "http://evil.example", "Access-Control-Request-Method": "GET"}); pre.Code != http.StatusForbidden {
			t.Fatalf("foreign preflight status %d", pre.Code)
		}
	})
}

// Each request has a trace of its own: the services' calls and log lines carry its ID, and the response names it.
func TestEachRequestHasAnIDTheServicesSee(t *testing.T) {
	h := newHarness("http://localhost:5173")
	var seen []string
	h.characters.list = func(ctx context.Context, _ kernel.AccountID) ([]character.Character, error) {
		seen = append(seen, trace.SpanContextFromContext(ctx).TraceID().String())
		return nil, nil
	}
	headers := map[string]string{"Authorization": "Bearer " + goodToken, "Origin": "http://localhost:5173"}
	first, second := h.do("GET", "/api/characters", "", headers), h.do("GET", "/api/characters", "", headers)
	ids := []string{first.Header().Get("X-Request-ID"), second.Header().Get("X-Request-ID")}
	if ids[0] == "" || ids[0] == ids[1] || !slices.Equal(seen, ids) {
		t.Fatalf("responses named %q, the service saw %q", ids, seen)
	}
	if !strings.Contains(first.Header().Get("Access-Control-Expose-Headers"), "X-Request-ID") {
		t.Fatalf("the browser may not read the ID: %v", first.Header())
	}
}

// Each /api request starts a sampled trace of its own, whatever trace context the caller sends; other paths keep none.
func TestEachAPIRequestStartsATraceOfItsOwn(t *testing.T) {
	h := newHarness()
	h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) {
		return nil, nil
	}
	const foreign = "0af7651916cd43dd8448eb211c80319c"
	before := len(spans.Ended())
	h.do("GET", "/api/characters", "", map[string]string{"Authorization": "Bearer " + goodToken, "traceparent": "00-" + foreign + "-b7ad6b7169203331-01"})
	h.do("GET", "/nowhere", "", nil)
	ended := spans.Ended()[before:]
	if len(ended) != 1 {
		t.Fatalf("%d spans kept, want the /api request's", len(ended))
	}
	if s := ended[0]; s.Name() != "GET /api/characters" || s.Parent().IsValid() || s.SpanContext().TraceID().String() == foreign {
		t.Fatalf("kept %s, parent %v, in trace %s", s.Name(), s.Parent(), s.SpanContext().TraceID())
	}
}

// http.ErrAbortHandler aborts the response on purpose; net/http closes the connection and logs nothing.
func TestAnAbortedHandlerIsLeftToNetHTTP(t *testing.T) {
	h := newHarness()
	h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) {
		panic(http.ErrAbortHandler)
	}
	var rec *httptest.ResponseRecorder
	func() {
		defer func() {
			if p := recover(); p != http.ErrAbortHandler { //nolint:errorlint // the sentinel itself, as net/http checks it
				t.Fatalf("recovered %v, want http.ErrAbortHandler passed on", p)
			}
		}()
		rec = h.do("GET", "/api/characters", "", authed())
	}()
	if rec != nil {
		t.Fatalf("answered %d; an aborted response gets no body", rec.Code)
	}
	logs := h.logs.String()
	if strings.Contains(logs, "panic recovered") || !strings.Contains(logs, `"aborted":true`) {
		t.Fatalf("log %s, want the request marked aborted and no panic recorded", logs)
	}
}

func TestPanicIsContained(t *testing.T) {
	h := newHarness()
	h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) {
		panic("kaboom: secret detail")
	}
	rec := h.do("GET", "/api/characters", "", authed())
	assertError(t, rec, http.StatusInternalServerError, model.Internal)
	if strings.Contains(rec.Body.String(), "kaboom") {
		t.Fatal("panic text reached the response")
	}
	logs := h.logs.String()
	for _, want := range []string{`"msg":"panic recovered"`, `"flow":"http"`, `"panic":"kaboom: secret detail"`, `"stack":`, `"method":"GET"`, `"path":"/api/characters"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("log lacks %s: %s", want, logs)
		}
	}
	if strings.Contains(logs, goodToken) {
		t.Fatal("the token reached a log line")
	}
}

func TestRequestLog(t *testing.T) {
	h := newHarness()
	h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) {
		return nil, nil
	}
	h.do("GET", "/api/characters", "", authed())
	h.do("GET", "/api/characters", "", nil)
	var lines []map[string]any
	for raw := range strings.SplitSeq(strings.TrimSpace(h.logs.String()), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			t.Fatalf("log line %q: %v", raw, err)
		}
		lines = append(lines, rec)
	}
	if len(lines) != 2 {
		t.Fatalf("%d log lines, want 2: %s", len(lines), h.logs.String())
	}
	for i, want := range []float64{200, 401} {
		l := lines[i]
		if l["msg"] != "request" || l["method"] != "GET" || l["path"] != "/api/characters" || l["status"] != want || l["duration_ms"] == nil {
			t.Fatalf("line %d = %v", i, l)
		}
	}
	if strings.Contains(h.logs.String(), goodToken) {
		t.Fatal("the token reached a log line")
	}
	if lines[0]["code"] != nil || lines[1]["code"] != float64(model.Unauthenticated.Code) || lines[1]["refusal"] != model.Unauthenticated.Name || lines[1]["err"] != nil {
		t.Fatalf("a success carries no code, a refusal its own and no cause: %v / %v", lines[0], lines[1])
	}
}

func TestRequestLogCarriesTheCauseOfAnUnexpectedError(t *testing.T) {
	h := newHarness()
	h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) {
		return nil, fmt.Errorf("list characters: %w", errInternal)
	}
	assertError(t, h.do("GET", "/api/characters", "", authed()), http.StatusInternalServerError, model.Internal)
	var l map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(h.logs.String())), &l); err != nil {
		t.Fatalf("log %q: %v", h.logs.String(), err)
	}
	if l["level"] != "ERROR" || l["code"] != float64(model.Internal.Code) || l["refusal"] != model.Internal.Name || l["err"] != "list characters: pq: connection reset" {
		t.Fatalf("log line %v, want level ERROR, code internal and the cause", l)
	}
}

func TestAServiceDownAnswers503AndWarns(t *testing.T) {
	h := newHarness()
	h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) {
		return nil, fmt.Errorf("%w: connection refused", transient.ErrUnavailable)
	}
	assertError(t, h.do("GET", "/api/characters", "", authed()), http.StatusServiceUnavailable, model.Unavailable)
	var l map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(h.logs.String())), &l); err != nil {
		t.Fatalf("log %q: %v", h.logs.String(), err)
	}
	if l["level"] != "WARN" || l["code"] != float64(model.Unavailable.Code) || l["refusal"] != model.Unavailable.Name || l["err"] == nil {
		t.Fatalf("log line %v, want level WARN, code unavailable and the cause", l)
	}
}

func TestResponsesAreJSON(t *testing.T) {
	h := newHarness()
	h.characters.list = func(context.Context, kernel.AccountID) ([]character.Character, error) {
		return nil, nil
	}
	rec := h.do("GET", "/api/characters", "", authed())
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type %q", ct)
	}
	body, _ := io.ReadAll(rec.Body)
	if !json.Valid(body) {
		t.Fatalf("not JSON: %s", body)
	}
}

func TestLogoutEverywhere(t *testing.T) {
	t.Run("revokes every session of the account", func(t *testing.T) {
		h := newHarness()
		rec := h.do("DELETE", "/api/sessions", "", authed())
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if len(h.accounts.revokedAccounts) != 1 || h.accounts.revokedAccounts[0] != accountID {
			t.Fatalf("revoked accounts %v", h.accounts.revokedAccounts)
		}
	})
	t.Run("needs a session", func(t *testing.T) {
		h := newHarness()
		assertError(t, h.do("DELETE", "/api/sessions", "", nil), http.StatusUnauthorized, model.Unauthenticated)
	})
}
