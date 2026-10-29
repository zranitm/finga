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
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	accountv1 "finga/api/gen/account/v1"
	characterv1 "finga/api/gen/character/v1"
	"finga/internal/kernel"
	"finga/pkg/auth"
	"finga/pkg/grpcx"
	"finga/services/gateway/internal/handler/api"
)

const goodToken = "tok-good-0123456789abcdef"

var (
	accountID   = kernel.NewAccountID()
	sessionID   = kernel.NewSessionID()
	principal   = auth.Principal{AccountID: uuid.UUID(accountID), SessionID: uuid.UUID(sessionID)}
	t0          = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	sampleChar  = &characterv1.Character{Id: kernel.NewCharacterID().String(), AccountId: accountID.String(), Name: "Ayla", Class: "warrior", Level: 1, Stats: &characterv1.Stats{Strength: 12, Dexterity: 5, Intelligence: 4, Agility: 5, Vitality: 10}, Hp: 204, MaxHp: 204, Mp: 28, MaxMp: 28, MapId: "town", X: 416, Y: 608, UpdatedAt: timestamppb.New(t0)}
	errInternal = errors.New("pq: connection reset")
)

type harness struct {
	accounts   *fakeAccounts
	characters *fakeCharacters
	sessions   *fakeSessions
	logs       *bytes.Buffer
	handler    http.Handler
}

func newHarness(origins ...string) *harness {
	h := &harness{
		accounts:   &fakeAccounts{},
		characters: &fakeCharacters{},
		sessions:   &fakeSessions{token: goodToken, principal: principal},
		logs:       &bytes.Buffer{},
	}
	h.handler = api.NewHandler(api.Dependencies{
		Accounts: h.accounts, Characters: h.characters, Sessions: h.sessions,
		Logger: slog.New(slog.NewJSONHandler(h.logs, nil)), CORSOrigins: origins,
	})
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

func assertError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	var body api.ErrorResponse
	decode(t, rec, &body)
	if body.Code != code || body.Message == "" {
		t.Fatalf("error body %+v, want code %s with a message", body, code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type %q", rec.Header().Get("Content-Type"))
	}
}

// errorRows are the service errors every endpoint must map the same way:
// each failure has its own code, and the status carries its class.
var errorRows = []struct {
	name   string
	err    error
	status int
	code   string
}{
	{"bad input is 400 with its own code", grpcx.Error(codes.InvalidArgument, accountv1.Reason_REASON_BAD_PASSWORD), http.StatusBadRequest, "bad_password"},
	{"unauthenticated is 401", grpcx.Error(codes.Unauthenticated, accountv1.Reason_REASON_UNAUTHENTICATED), http.StatusUnauthorized, "unauthenticated"},
	{"a missing character is 404 character_not_found", grpcx.Error(codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND), http.StatusNotFound, "character_not_found"},
	{"a taken username is 409 username_taken", grpcx.Error(codes.AlreadyExists, accountv1.Reason_REASON_USERNAME_TAKEN), http.StatusConflict, "username_taken"},
	{"too many logins is 429 login_rate_limited", grpcx.Error(codes.ResourceExhausted, accountv1.Reason_REASON_LOGIN_RATE_LIMITED), http.StatusTooManyRequests, "login_rate_limited"},
	{"a service that cannot be reached is 500 internal", status.Error(codes.Unavailable, "connection refused"), http.StatusInternalServerError, "internal"},
	{"a wrapped error keeps its code and hides its cause", fmt.Errorf("create: %w", fmt.Errorf("%w: %w", grpcx.Error(codes.AlreadyExists, characterv1.Reason_REASON_NAME_TAKEN), errInternal)), http.StatusConflict, "name_taken"},
	{"a plain error is 500 internal without its text", errInternal, http.StatusInternalServerError, "internal"},
}

func TestRegister(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		h := newHarness()
		var got *accountv1.RegisterRequest
		h.accounts.register = func(_ context.Context, in *accountv1.RegisterRequest) (*accountv1.RegisterResponse, error) {
			got = in
			return &accountv1.RegisterResponse{Account: &accountv1.Account{Id: accountID.String(), Username: in.GetUsername(), CreatedAt: timestamppb.New(t0)}}, nil
		}
		rec := h.do("POST", "/api/accounts", `{"username":"alice","password":"correct horse"}`, nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if got.GetUsername() != "alice" || got.GetPassword() != "correct horse" {
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
			code string
		}{
			{"not JSON", `{username`, "malformed_body"},
			{"wrong type", `{"username":1,"password":"x"}`, "malformed_body"},
			{"missing username", `{"password":"correct horse"}`, "missing_field"},
			{"missing password", `{"username":"alice"}`, "missing_field"},
			{"empty body", ``, "malformed_body"},
			{"unknown field", `{"username":"alice","password":"correct horse","admin":true}`, "malformed_body"},
			{"two JSON documents", `{"username":"alice","password":"correct horse"}{}`, "malformed_body"},
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
		assertError(t, h.do("POST", "/api/accounts", big, nil), http.StatusRequestEntityTooLarge, "body_too_large")
	})
	t.Run("service errors", func(t *testing.T) {
		for _, tt := range errorRows {
			t.Run(tt.name, func(t *testing.T) {
				h := newHarness()
				h.accounts.register = func(context.Context, *accountv1.RegisterRequest) (*accountv1.RegisterResponse, error) {
					return nil, tt.err
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
	t.Run("happy path passes the client IP and returns the token once", func(t *testing.T) {
		h := newHarness()
		var got *accountv1.LoginRequest
		h.accounts.login = func(_ context.Context, in *accountv1.LoginRequest) (*accountv1.LoginResponse, error) {
			got = in
			return &accountv1.LoginResponse{Token: "secret-token"}, nil
		}
		rec := h.do("POST", "/api/sessions", `{"username":"alice","password":"correct horse"}`, nil)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if got.GetUsername() != "alice" || got.GetPassword() != "correct horse" || got.GetIp() == "" {
			t.Fatalf("service got %+v", got)
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
		assertError(t, h.do("POST", "/api/sessions", `{"username":"alice"}`, nil), http.StatusBadRequest, "missing_field")
	})
	t.Run("service errors", func(t *testing.T) {
		for _, tt := range errorRows {
			t.Run(tt.name, func(t *testing.T) {
				h := newHarness()
				h.accounts.login = func(context.Context, *accountv1.LoginRequest) (*accountv1.LoginResponse, error) { return nil, tt.err }
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
		if len(h.sessions.revoked) != 1 || h.sessions.revoked[0] != uuid.UUID(sessionID) {
			t.Fatalf("revoked %v", h.sessions.revoked)
		}
	})
	t.Run("needs a session", func(t *testing.T) {
		h := newHarness()
		assertError(t, h.do("DELETE", "/api/sessions/current", "", nil), http.StatusUnauthorized, "unauthenticated")
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
			assertError(t, rec, http.StatusUnauthorized, "unauthenticated")
			if rec.Header().Get("WWW-Authenticate") != "Bearer" {
				t.Fatalf("WWW-Authenticate %q", rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
	t.Run("a session store failure is 500, not 401", func(t *testing.T) {
		h := newHarness()
		h.sessions.validate = func(string) (auth.Principal, error) { return auth.Principal{}, errInternal }
		assertError(t, h.do("GET", "/api/characters", "", authed()), http.StatusInternalServerError, "internal")
	})
	t.Run("bearer is matched regardless of case", func(t *testing.T) {
		h := newHarness()
		h.characters.list = func(context.Context, *characterv1.ListRequest) (*characterv1.ListResponse, error) {
			return &characterv1.ListResponse{}, nil
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
		var gotOwner string
		h.characters.list = func(_ context.Context, in *characterv1.ListRequest) (*characterv1.ListResponse, error) {
			gotOwner = in.GetAccountId()
			return &characterv1.ListResponse{Characters: []*characterv1.Character{sampleChar}}, nil
		}
		rec := h.do("GET", "/api/characters", "", authed())
		if rec.Code != http.StatusOK || gotOwner != accountID.String() {
			t.Fatalf("status %d owner %s", rec.Code, gotOwner)
		}
		var body api.CharactersResponse
		decode(t, rec, &body)
		if len(body.Characters) != 1 {
			t.Fatalf("body %+v", body)
		}
		c := body.Characters[0]
		if c.ID != sampleChar.GetId() || c.Name != "Ayla" || c.Class != "warrior" || c.Level != 1 || c.Stats.STR != 12 || c.HP != 204 || c.MP != 28 || c.MapID != "town" {
			t.Fatalf("character %+v", c)
		}
	})
	t.Run("an empty list is an empty array, not null", func(t *testing.T) {
		h := newHarness()
		h.characters.list = func(context.Context, *characterv1.ListRequest) (*characterv1.ListResponse, error) {
			return &characterv1.ListResponse{}, nil
		}
		rec := h.do("GET", "/api/characters", "", authed())
		if !strings.Contains(rec.Body.String(), `"characters":[]`) {
			t.Fatalf("body %s", rec.Body.String())
		}
	})
	t.Run("create", func(t *testing.T) {
		h := newHarness()
		var got *characterv1.CreateRequest
		h.characters.create = func(_ context.Context, in *characterv1.CreateRequest) (*characterv1.CreateResponse, error) {
			got = in
			return &characterv1.CreateResponse{Character: sampleChar}, nil
		}
		rec := h.do("POST", "/api/characters", `{"name":"Ayla","class":"warrior"}`, authed())
		if rec.Code != http.StatusCreated {
			t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
		}
		if got.GetName() != "Ayla" || got.GetClass() != "warrior" || got.GetAccountId() != accountID.String() {
			t.Fatalf("service got %+v", got)
		}
	})
	t.Run("create with malformed input", func(t *testing.T) {
		tests := []struct {
			name string
			body string
			code string
		}{
			{"missing class", `{"name":"Ayla"}`, "missing_field"},
			{"missing name", `{"class":"warrior"}`, "missing_field"},
			{"not JSON", `nope`, "malformed_body"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				h := newHarness()
				assertError(t, h.do("POST", "/api/characters", tt.body, authed()), http.StatusBadRequest, tt.code)
			})
		}
	})
	t.Run("create passes an unknown class to the service, which owns that rule", func(t *testing.T) {
		h := newHarness()
		h.characters.create = func(context.Context, *characterv1.CreateRequest) (*characterv1.CreateResponse, error) {
			return nil, grpcx.Error(codes.InvalidArgument, characterv1.Reason_REASON_CLASS_UNAVAILABLE)
		}
		assertError(t, h.do("POST", "/api/characters", `{"name":"Ayla","class":"paladin"}`, authed()), http.StatusBadRequest, "class_unavailable")
	})
	t.Run("service errors", func(t *testing.T) {
		for _, tt := range errorRows {
			t.Run(tt.name, func(t *testing.T) {
				h := newHarness()
				h.characters.create = func(context.Context, *characterv1.CreateRequest) (*characterv1.CreateResponse, error) {
					return nil, tt.err
				}
				h.characters.list = func(context.Context, *characterv1.ListRequest) (*characterv1.ListResponse, error) { return nil, tt.err }
				assertError(t, h.do("POST", "/api/characters", `{"name":"Ayla","class":"warrior"}`, authed()), tt.status, tt.code)
				assertError(t, h.do("GET", "/api/characters", "", authed()), tt.status, tt.code)
			})
		}
	})
}

func TestSettings(t *testing.T) {
	t.Run("read", func(t *testing.T) {
		h := newHarness()
		h.accounts.settings = func(_ context.Context, in *accountv1.GetSettingsRequest) (*accountv1.GetSettingsResponse, error) {
			if in.GetAccountId() != accountID.String() {
				t.Fatalf("id %s", in.GetAccountId())
			}
			return &accountv1.GetSettingsResponse{Settings: &accountv1.Settings{ControlPreset: "wasd"}}, nil
		}
		rec := h.do("GET", "/api/settings", "", authed())
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d", rec.Code)
		}
		var body api.SettingsResponse
		decode(t, rec, &body)
		if body.ControlPreset != "wasd" {
			t.Fatalf("body %+v", body)
		}
	})
	t.Run("write returns the saved settings", func(t *testing.T) {
		h := newHarness()
		var saved *accountv1.UpdateSettingsRequest
		h.accounts.updateSettings = func(_ context.Context, in *accountv1.UpdateSettingsRequest) (*accountv1.UpdateSettingsResponse, error) {
			saved = in
			return &accountv1.UpdateSettingsResponse{}, nil
		}
		rec := h.do("PUT", "/api/settings", `{"control_preset":"wasd"}`, authed())
		if rec.Code != http.StatusOK || saved.GetSettings().GetControlPreset() != "wasd" || saved.GetAccountId() != accountID.String() {
			t.Fatalf("status %d saved %+v", rec.Code, saved)
		}
		var body api.SettingsResponse
		decode(t, rec, &body)
		if body.ControlPreset != "wasd" {
			t.Fatalf("body %+v", body)
		}
	})
	t.Run("write with malformed input", func(t *testing.T) {
		h := newHarness()
		assertError(t, h.do("PUT", "/api/settings", `{}`, authed()), http.StatusBadRequest, "missing_field")
	})
	t.Run("an unknown preset is the service's rule", func(t *testing.T) {
		h := newHarness()
		h.accounts.updateSettings = func(context.Context, *accountv1.UpdateSettingsRequest) (*accountv1.UpdateSettingsResponse, error) {
			return nil, grpcx.Error(codes.InvalidArgument, accountv1.Reason_REASON_BAD_CONTROL_PRESET)
		}
		assertError(t, h.do("PUT", "/api/settings", `{"control_preset":"vim"}`, authed()), http.StatusBadRequest, "bad_control_preset")
	})
	t.Run("service errors", func(t *testing.T) {
		for _, tt := range errorRows {
			t.Run(tt.name, func(t *testing.T) {
				h := newHarness()
				h.accounts.settings = func(context.Context, *accountv1.GetSettingsRequest) (*accountv1.GetSettingsResponse, error) {
					return nil, tt.err
				}
				h.accounts.updateSettings = func(context.Context, *accountv1.UpdateSettingsRequest) (*accountv1.UpdateSettingsResponse, error) {
					return nil, tt.err
				}
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
	}
	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			rec := h.do(tt.method, tt.path, "", authed())
			if rec.Code != tt.status {
				t.Fatalf("status %d, want %d", rec.Code, tt.status)
			}
			var body api.ErrorResponse
			decode(t, rec, &body)
			if body.Code == "" {
				t.Fatalf("no JSON error body: %s", rec.Body.String())
			}
		})
	}
}

func TestCORS(t *testing.T) {
	t.Run("with no allowed origins, no CORS headers and preflight is refused", func(t *testing.T) {
		h := newHarness()
		h.characters.list = func(context.Context, *characterv1.ListRequest) (*characterv1.ListResponse, error) {
			return &characterv1.ListResponse{}, nil
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
		h.characters.list = func(context.Context, *characterv1.ListRequest) (*characterv1.ListResponse, error) {
			return &characterv1.ListResponse{}, nil
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

func TestPanicIsContained(t *testing.T) {
	h := newHarness()
	h.characters.list = func(context.Context, *characterv1.ListRequest) (*characterv1.ListResponse, error) {
		panic("kaboom: secret detail")
	}
	rec := h.do("GET", "/api/characters", "", authed())
	assertError(t, rec, http.StatusInternalServerError, "internal")
	if strings.Contains(rec.Body.String(), "kaboom") {
		t.Fatal("panic text reached the response")
	}
	logs := h.logs.String()
	if !strings.Contains(logs, "kaboom") || !strings.Contains(logs, "stack") {
		t.Fatalf("the panic and its stack were not logged: %s", logs)
	}
	if strings.Contains(logs, goodToken) {
		t.Fatal("the token reached a log line")
	}
}

func TestRequestLog(t *testing.T) {
	h := newHarness()
	h.characters.list = func(context.Context, *characterv1.ListRequest) (*characterv1.ListResponse, error) {
		return &characterv1.ListResponse{}, nil
	}
	h.do("GET", "/api/characters", "", authed())
	h.do("GET", "/api/characters", "", nil)
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
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
	if lines[0]["code"] != nil || lines[1]["code"] != "unauthenticated" || lines[1]["err"] != nil {
		t.Fatalf("a success carries no code, a refusal its own and no cause: %v / %v", lines[0], lines[1])
	}
}

func TestRequestLogCarriesTheCauseOfAnUnexpectedError(t *testing.T) {
	h := newHarness()
	h.characters.list = func(context.Context, *characterv1.ListRequest) (*characterv1.ListResponse, error) {
		return nil, fmt.Errorf("list characters: %w", errInternal)
	}
	assertError(t, h.do("GET", "/api/characters", "", authed()), http.StatusInternalServerError, "internal")
	var l map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(h.logs.String())), &l); err != nil {
		t.Fatalf("log %q: %v", h.logs.String(), err)
	}
	if l["level"] != "ERROR" || l["code"] != "internal" || l["err"] != "list characters: pq: connection reset" {
		t.Fatalf("log line %v, want level ERROR, code internal and the cause", l)
	}
}

func TestResponsesAreJSON(t *testing.T) {
	h := newHarness()
	h.characters.list = func(context.Context, *characterv1.ListRequest) (*characterv1.ListResponse, error) {
		return &characterv1.ListResponse{}, nil
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
		if len(h.sessions.revokedAccounts) != 1 || h.sessions.revokedAccounts[0] != uuid.UUID(accountID) {
			t.Fatalf("revoked accounts %v", h.sessions.revokedAccounts)
		}
	})
	t.Run("needs a session", func(t *testing.T) {
		h := newHarness()
		assertError(t, h.do("DELETE", "/api/sessions", "", nil), http.StatusUnauthorized, "unauthenticated")
	})
}
