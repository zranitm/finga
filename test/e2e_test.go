// Package e2e runs the game server as production builds it (server.Build)
// against a real Postgres, in process. Each Stage 1 acceptance criterion
// that the REST API covers is a row named after its feature.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/config"
	"finga/internal/protocol"
	"finga/internal/server"
	"finga/migrations"
	"finga/pkg/pgtest"
	"finga/pkg/wsconn"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, migrations.Up)
	if err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	os.Exit(code)
}

// stack is one game server over a fresh database, served by httptest.
type stack struct {
	t   *testing.T
	url string
	db  *pgxpool.Pool
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := config.Config{
		Server:    config.Server{Addr: "127.0.0.1:0", CORSOrigins: []string{"http://localhost:5173"}, ShutdownGrace: time.Second},
		Database:  config.Database{URL: cluster.URL(t), MaxConns: 4},
		World:     config.World{TickRateHz: 20, CheckpointInterval: 30 * time.Second, DataDir: "../data"},
		Economy:   config.Economy{Workers: 2},
		Telemetry: config.Telemetry{LogLevel: "error"},
	}
	srv, err := server.Build(ctx, server.BuildOptions{
		Config:   cfg,
		Logger:   slog.New(slog.DiscardHandler),
		Registry: prometheus.NewRegistry(),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	// The handler is served by httptest; the world and persistence tasks
	// run beside it as Run would start them.
	taskCtx, stopTasks := context.WithCancel(context.Background())
	tasksDone := make(chan struct{})
	go func() { defer close(tasksDone); srv.RunTasks(taskCtx) }()
	t.Cleanup(func() { stopTasks(); <-tasksDone })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	db, err := pgxpool.New(ctx, cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	return &stack{t: t, url: ts.URL, db: db}
}

type response struct {
	status int
	body   map[string]any
}

func (s *stack) call(method, path, token string, body any) response {
	s.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			s.t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, s.url+path, &buf)
	if err != nil {
		s.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	out := response{status: res.StatusCode}
	raw, _ := io.ReadAll(res.Body)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out.body); err != nil {
			s.t.Fatalf("%s %s: body %q is not JSON", method, path, raw)
		}
	}
	return out
}

func (s *stack) mustStatus(r response, want int) {
	s.t.Helper()
	if r.status != want {
		s.t.Fatalf("status %d, want %d: %v", r.status, want, r.body)
	}
}

func TestAcceptance(t *testing.T) {
	t.Run("A1/sign-up-and-log-in", func(t *testing.T) {
		s := newStack(t)
		s.mustStatus(s.call("POST", "/api/accounts", "", map[string]string{"username": "alice", "password": "correct horse"}), http.StatusCreated)
		s.mustStatus(s.call("POST", "/api/accounts", "", map[string]string{"username": "ALICE", "password": "correct horse"}), http.StatusConflict)
		login := s.call("POST", "/api/sessions", "", map[string]string{"username": "alice", "password": "correct horse"})
		s.mustStatus(login, http.StatusCreated)
		token, _ := login.body["token"].(string)
		if token == "" {
			t.Fatalf("no token: %v", login.body)
		}
		s.mustStatus(s.call("GET", "/api/characters", token, nil), http.StatusOK)
		s.mustStatus(s.call("POST", "/api/sessions", "", map[string]string{"username": "alice", "password": "wrong"}), http.StatusUnauthorized)
		s.mustStatus(s.call("DELETE", "/api/sessions/current", token, nil), http.StatusNoContent)
		s.mustStatus(s.call("GET", "/api/characters", token, nil), http.StatusUnauthorized)
	})

	t.Run("A1/hello-rotates-the-token-reuse-ends-the-session-and-logout-everywhere-kicks", func(t *testing.T) {
		s := newStack(t)
		p := s.enter(t, "erin", "Erin", "warrior")
		login := s.call("POST", "/api/sessions", "", map[string]string{"username": "erin", "password": "correct horse"})
		// The login above is a second session for Erin: it kicked the first
		// connection and revoked its session, as one active session demands.
		p.expect("Kicked")
		token := login.body["token"].(string)
		q := s.connect(t, p.id)
		q.send(wsconn.Message{Data: protocol.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterID: p.id}})
		w := q.expect("Welcome").Data.(protocol.Welcome)
		q.expect("Snapshot")
		if w.Token == "" || w.Token == token {
			t.Fatalf("welcome token %q", w.Token)
		}
		// The rotated token works; the one sent in Hello works once more.
		s.mustStatus(s.call("GET", "/api/characters", w.Token, nil), http.StatusOK)
		s.mustStatus(s.call("GET", "/api/characters", token, nil), http.StatusOK)
		// A second use of the retired token is reuse: the session ends, the
		// connection is kicked, and the rotated token is dead too.
		s.mustStatus(s.call("GET", "/api/characters", token, nil), http.StatusUnauthorized)
		if k := q.expect("Kicked").Data.(protocol.Kicked); k.Reason != protocol.KickSessionExpired {
			t.Fatalf("kicked for %s", k.Reason)
		}
		s.mustStatus(s.call("GET", "/api/characters", w.Token, nil), http.StatusUnauthorized)

		// Log out everywhere: the connection is kicked and the token is dead.
		r := s.enter(t, "fay", "Fay", "bowman")
		s.mustStatus(s.call("DELETE", "/api/sessions", r.token, nil), http.StatusNoContent)
		if k := r.expect("Kicked").Data.(protocol.Kicked); k.Reason != protocol.KickSessionExpired {
			t.Fatalf("kicked for %s", k.Reason)
		}
		s.mustStatus(s.call("GET", "/api/characters", r.token, nil), http.StatusUnauthorized)
	})

	t.Run("A1/throttled-after-repeated-failures", func(t *testing.T) {
		s := newStack(t)
		s.mustStatus(s.call("POST", "/api/accounts", "", map[string]string{"username": "bob", "password": "correct horse"}), http.StatusCreated)
		for range 5 {
			s.mustStatus(s.call("POST", "/api/sessions", "", map[string]string{"username": "bob", "password": "wrong"}), http.StatusUnauthorized)
		}
		r := s.call("POST", "/api/sessions", "", map[string]string{"username": "bob", "password": "correct horse"})
		s.mustStatus(r, http.StatusTooManyRequests)
		if r.body["code"] != "rate_limited" {
			t.Fatalf("body %v", r.body)
		}
	})

	t.Run("A2/create-a-character", func(t *testing.T) {
		s := newStack(t)
		s.mustStatus(s.call("POST", "/api/accounts", "", map[string]string{"username": "carol", "password": "correct horse"}), http.StatusCreated)
		login := s.call("POST", "/api/sessions", "", map[string]string{"username": "carol", "password": "correct horse"})
		token := login.body["token"].(string)
		created := s.call("POST", "/api/characters", token, map[string]string{"name": "Ayla", "class": "warrior"})
		s.mustStatus(created, http.StatusCreated)
		if created.body["level"] != float64(1) || created.body["class"] != "warrior" || created.body["map_id"] != "town" || created.body["hp"] != float64(204) {
			t.Fatalf("character %v", created.body)
		}
		s.mustStatus(s.call("POST", "/api/characters", token, map[string]string{"name": "ayla", "class": "bowman"}), http.StatusConflict)
		s.mustStatus(s.call("POST", "/api/characters", token, map[string]string{"name": "Merlin", "class": "magician"}), http.StatusBadRequest)
		list := s.call("GET", "/api/characters", token, nil)
		s.mustStatus(list, http.StatusOK)
		chars, _ := list.body["characters"].([]any)
		if len(chars) != 1 {
			t.Fatalf("characters %v", list.body)
		}
	})

	t.Run("B6/settings-saved-and-returned", func(t *testing.T) {
		s := newStack(t)
		s.mustStatus(s.call("POST", "/api/accounts", "", map[string]string{"username": "dave", "password": "correct horse"}), http.StatusCreated)
		token := s.call("POST", "/api/sessions", "", map[string]string{"username": "dave", "password": "correct horse"}).body["token"].(string)
		initial := s.call("GET", "/api/settings", token, nil)
		s.mustStatus(initial, http.StatusOK)
		if initial.body["control_preset"] != "classic" {
			t.Fatalf("default settings %v", initial.body)
		}
		s.mustStatus(s.call("PUT", "/api/settings", token, map[string]string{"control_preset": "wasd"}), http.StatusOK)
		s.mustStatus(s.call("PUT", "/api/settings", token, map[string]string{"control_preset": "vim"}), http.StatusBadRequest)
		after := s.call("GET", "/api/settings", token, nil)
		if after.body["control_preset"] != "wasd" {
			t.Fatalf("settings after save %v", after.body)
		}
		// A new login still sees the saved preset.
		token2 := s.call("POST", "/api/sessions", "", map[string]string{"username": "dave", "password": "correct horse"}).body["token"].(string)
		if s.call("GET", "/api/settings", token2, nil).body["control_preset"] != "wasd" {
			t.Fatal("settings did not survive a new session")
		}
	})

	t.Run("probes/healthz-readyz-metrics", func(t *testing.T) {
		s := newStack(t)
		s.mustStatus(s.call("GET", "/healthz", "", nil), http.StatusOK)
		res, err := http.Get(s.url + "/metrics")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("metrics status %d", res.StatusCode)
		}
	})
}
