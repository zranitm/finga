// Package e2e runs the whole game, built as production builds it, against a real Postgres.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul/sdk/freeport"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	clientv1 "finga/api/gen/client/v1"
	"finga/cmd/finga/app"
	migrateapp "finga/cmd/migrate/app"
	"finga/config"
	"finga/internal/host"
	"finga/internal/protocol"
	accountconfig "finga/modules/account/config"
	gatewayconfig "finga/modules/gateway/config"
	"finga/modules/item"
	itemconfig "finga/modules/item/config"
	socialconfig "finga/modules/social/config"
	worldconfig "finga/modules/world/config"
	"finga/pkg/dbx"
	"finga/pkg/pgtest"
	"finga/pkg/runner/runnertest"
	"finga/pkg/wsconn"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, func(ctx context.Context, pool *pgxpool.Pool) error {
		_, err := migrateapp.Apply(ctx, pool) // every module's set, as cmd/migrate applies them
		return err
	})
	if err != nil {
		panic(err)
	}
	if binDir, err = os.MkdirTemp("", "finga-e2e-bin"); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = cluster.Close(ctx)
	_ = os.RemoveAll(binDir)
	os.Exit(code)
}

// stack is the game over a fresh database: cmd/finga's app, every module in one process, on loopback ports.
type stack struct {
	t     *testing.T
	url   string // the players' listener
	ops   string // /healthz, /readyz and /metrics
	admin string // the private admin API, on as in dev
	db    *pgxpool.Pool
	items *item.Module // the item module on a host of its own over the same database, for rows that call it directly
	data  string       // the game data every module loads
	cfg   config.Config
	build host.Build[config.Config] // cmd/finga's app, or a test's composition
	stop  func()
}

const grace = time.Second

// itemConfig is the item module's, for the game and for the module a row calls directly.
var itemConfig = itemconfig.Config{
	IdempotencyKeys: itemconfig.IdempotencyKeys{Keep: 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 1000},
	Outbox:          itemconfig.Outbox{PublishedKeep: 24 * time.Hour, UnpublishedKeep: 7 * 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 1000},
}

// cheapPasswords keeps the stack's hashing fast; production's cost is in config/config.yaml.
var cheapPasswords = accountconfig.Password{Time: 1, MemoryKiB: 8 * 1024, Threads: 1, Concurrent: 2, Wait: time.Second}

func newStack(t *testing.T) *stack {
	t.Helper()
	return newStackOver(t, "../data")
}

// newStackOver runs cmd/finga's app over the game data in data.
func newStackOver(t *testing.T, data string) *stack {
	t.Helper()
	return newStackBuilt(t, data, app.Build)
}

// newStackBuilt runs the game composed by build over the game data in data.
func newStackBuilt(t *testing.T, data string, build host.Build[config.Config]) *stack {
	t.Helper()
	at := freeAddrs(t, 3)
	s := &stack{t: t, url: "http://" + at[0], ops: "http://" + at[1], admin: "http://" + at[2], data: data, build: build, cfg: config.Config{
		HTTP:          host.HTTPSection{Addr: at[0], CORSOrigins: []string{"http://localhost:5173"}},
		Ops:           host.OpsSection{Addr: at[1]},
		Admin:         config.AdminSection{Addr: at[2], Enabled: true},
		ShutdownGrace: grace,
		DataDir:       data,
		Database:      dbx.Config{URL: cluster.URL(t), MaxConns: 12},
		Gateway: gatewayconfig.Config{
			WS:       gatewayconfig.WS{HelloTimeout: 5 * time.Second, CommandQueue: 64},
			Sessions: gatewayconfig.Sessions{JoinTimeout: 5 * time.Second, CheckInterval: time.Minute},
		},
		Account: accountconfig.Config{Password: cheapPasswords, Sessions: accountconfig.Sessions{Keep: 30 * 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 1000}},
		Item:    itemConfig,
		World:   worldconfig.Config{TickRateHz: 20, CheckpointInterval: 30 * time.Second, FlushTimeout: 10 * time.Second, ItemCalls: worldconfig.ItemCalls{Workers: 2, BatchMax: 32}},
		Social:  socialconfig.Config{Members: socialconfig.Members{Keep: time.Hour, PruneEvery: time.Hour, PruneBatch: 1000}},
	}}
	s.start()
	t.Cleanup(func() { s.stop() })
	pool, err := pgxpool.New(context.Background(), s.cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	s.db, s.items = pool, itemsOver(t, s.cfg)
	return s
}

// itemsOver is the item module on a host never run, over cfg's database: what a row asks of it goes through the module's rules, as a world's call does.
func itemsOver(t *testing.T, cfg config.Config) *item.Module {
	t.Helper()
	h, err := host.New(host.Options{Name: "items", Settings: host.Settings{DataDir: cfg.DataDir, Database: cfg.Database}, Logger: runnertest.NewLog().Logger(), Registry: prometheus.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	data, err := h.GameData()
	if err != nil {
		t.Fatal(err)
	}
	m, err := h.Module("item")
	if err != nil {
		t.Fatal(err)
	}
	items, err := item.New(t.Context(), item.Deps{Module: m, Config: cfg.Item, Data: data})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// freeAddrs are n loopback addresses outside the kernel's ephemeral range, so a restart can bind them again.
func freeAddrs(t *testing.T, n int) []string {
	t.Helper()
	var addrs []string
	for _, port := range freeport.GetN(t, n) {
		addrs = append(addrs, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	}
	return addrs
}

// start runs the game until the test ends, on the stack's addresses each time.
func (s *stack) start() {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	settings, err := s.cfg.Host("finga")
	if err != nil {
		s.t.Fatal(err)
	}
	log := runnertest.NewLog()
	h, err := host.New(host.Options{Name: "finga", Settings: settings, Logger: log.Logger(), Registry: prometheus.NewRegistry()})
	if err != nil {
		s.t.Fatal(err)
	}
	if err := s.build(ctx, h, s.cfg); err != nil {
		h.Close()
		s.t.Fatal(err)
	}
	p := runnertest.Start(s.t, log, h.Run, "http", "ops")
	s.stop = func() {
		if err := p.Stop(); err != nil {
			s.t.Errorf("the game stopped with %v", err)
		}
		h.Close()
		s.stop = func() {}
	}
}

// restart stops the game as a deploy does, then starts a fresh process on the same addresses: what it held in memory is gone, what it saved stays.
func (s *stack) restart() {
	s.t.Helper()
	s.stop()
	s.start()
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
	t.Parallel()
	t.Run("A1/sign-up-and-log-in", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
		s := newStack(t)
		p := s.enter(t, "erin", "Erin", "warrior")
		login := s.call("POST", "/api/sessions", "", map[string]string{"username": "erin", "password": "correct horse"})
		// The second login kicked the first connection and revoked its session.
		p.expect("Kicked")
		token := login.body["token"].(string)
		q := s.connect(t, p.id)
		q.send(wsconn.Message{Data: &clientv1.Hello{Token: token, ProtocolVersion: protocol.Version, CharacterId: p.id}})
		w := q.expect("Welcome").Data.(*clientv1.Welcome)
		q.expect("Snapshot")
		if w.Token == "" || w.Token == token {
			t.Fatalf("welcome token %q", w.Token)
		}
		// The rotated token works; the one sent in Hello works once more.
		s.mustStatus(s.call("GET", "/api/characters", w.Token, nil), http.StatusOK)
		s.mustStatus(s.call("GET", "/api/characters", token, nil), http.StatusOK)
		// Reusing the retired token ends the session and kills the rotated token too.
		s.mustStatus(s.call("GET", "/api/characters", token, nil), http.StatusUnauthorized)
		if k := q.expect("Kicked").Data.(*clientv1.Kicked); k.Reason != clientv1.KickReason_KICK_REASON_SESSION_EXPIRED {
			t.Fatalf("kicked for %s", k.Reason)
		}
		s.mustStatus(s.call("GET", "/api/characters", w.Token, nil), http.StatusUnauthorized)

		r := s.enter(t, "fay", "Fay", "bowman")
		s.mustStatus(s.call("DELETE", "/api/sessions", r.token, nil), http.StatusNoContent)
		if k := r.expect("Kicked").Data.(*clientv1.Kicked); k.Reason != clientv1.KickReason_KICK_REASON_SESSION_EXPIRED {
			t.Fatalf("kicked for %s", k.Reason)
		}
		s.mustStatus(s.call("GET", "/api/characters", r.token, nil), http.StatusUnauthorized)
	})

	t.Run("A1/throttled-after-repeated-failures", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		s.mustStatus(s.call("POST", "/api/accounts", "", map[string]string{"username": "bob", "password": "correct horse"}), http.StatusCreated)
		for range 5 {
			s.mustStatus(s.call("POST", "/api/sessions", "", map[string]string{"username": "bob", "password": "wrong"}), http.StatusUnauthorized)
		}
		r := s.call("POST", "/api/sessions", "", map[string]string{"username": "bob", "password": "correct horse"})
		s.mustStatus(r, http.StatusTooManyRequests)
		if r.body["code"] != float64(code("LOGIN_RATE_LIMITED")) {
			t.Fatalf("body %v", r.body)
		}
	})

	t.Run("A2/create-a-character", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		s.mustStatus(s.call("POST", "/api/accounts", "", map[string]string{"username": "carol", "password": "correct horse"}), http.StatusCreated)
		login := s.call("POST", "/api/sessions", "", map[string]string{"username": "carol", "password": "correct horse"})
		token := login.body["token"].(string)
		created := s.call("POST", "/api/characters", token, map[string]string{"name": "Ayla"})
		s.mustStatus(created, http.StatusCreated)
		if created.body["level"] != float64(1) || created.body["class"] != "beginner" || created.body["map_id"] != "town" || created.body["hp"] != float64(135) {
			t.Fatalf("character %v; want a level 1 Beginner in town at full HP", created.body)
		}
		s.mustStatus(s.call("POST", "/api/characters", token, map[string]string{"name": "ayla"}), http.StatusConflict)
		// Every character starts a Beginner: a class in the body is refused.
		s.mustStatus(s.call("POST", "/api/characters", token, map[string]string{"name": "Merlin", "class": "magician"}), http.StatusBadRequest)
		list := s.call("GET", "/api/characters", token, nil)
		s.mustStatus(list, http.StatusOK)
		chars, _ := list.body["characters"].([]any)
		if len(chars) != 1 {
			t.Fatalf("characters %v", list.body)
		}
	})

	t.Run("settings-saved-and-returned", func(t *testing.T) {
		t.Parallel()
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
		token2 := s.call("POST", "/api/sessions", "", map[string]string{"username": "dave", "password": "correct horse"}).body["token"].(string)
		if s.call("GET", "/api/settings", token2, nil).body["control_preset"] != "wasd" {
			t.Fatal("settings did not survive a new session")
		}
	})

	t.Run("probes/on-the-ops-listener-only", func(t *testing.T) {
		t.Parallel()
		s := newStack(t)
		for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
			if status, _ := runnertest.Get(t, s.ops+path); status != http.StatusOK {
				t.Errorf("ops %s: status %d", path, status)
			}
			s.mustStatus(s.call("GET", path, "", nil), http.StatusNotFound)
		}
	})
}
