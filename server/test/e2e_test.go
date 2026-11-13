// Package e2e runs the whole game, built as production builds it, against a real Postgres.
package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
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
	itemv1 "finga/api/gen/item/v1"
	migrateapp "finga/cmd/migrate/app"
	"finga/internal/host"
	"finga/internal/protocol"
	"finga/pkg/dbx"
	"finga/pkg/grpcx"
	"finga/pkg/pgtest"
	"finga/pkg/runner/runnertest"
	"finga/pkg/wsconn"
	accountapp "finga/services/account/app"
	accountconfig "finga/services/account/config"
	characterapp "finga/services/character/app"
	characterconfig "finga/services/character/config"
	"finga/services/gateway/app"
	gatewayconfig "finga/services/gateway/config"
	itemapp "finga/services/item/app"
	itemconfig "finga/services/item/config"
	socialapp "finga/services/social/app"
	socialconfig "finga/services/social/config"
	worldapp "finga/services/world/app"
	worldconfig "finga/services/world/config"
)

var cluster *pgtest.Cluster

func TestMain(m *testing.M) {
	ctx := context.Background()
	var err error
	cluster, err = pgtest.Start(ctx, func(ctx context.Context, pool *pgxpool.Pool) error {
		_, err := migrateapp.Apply(ctx, pool) // every service's set, as cmd/migrate applies them
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

// stack is the game over a fresh database, every process on loopback ports.
type stack struct {
	t      *testing.T
	url    string // the players' listener
	ops    string // the gateway's /healthz, /readyz and /metrics
	db     *pgxpool.Pool
	items  itemv1.ItemServiceClient // the stack's item service, for rows that call it directly
	social *service                 // restarted by the rows that check the gateway survives it
	world  *service                 // likewise
	data   string                   // the game data every service loads
}

const grace = time.Second

// cheapPasswords keeps the stack's hashing fast; production's cost is in the account's config.yaml.
var cheapPasswords = accountconfig.Password{Time: 1, MemoryKiB: 8 * 1024, Threads: 1, Concurrent: 2, Wait: time.Second}

func newStack(t *testing.T) *stack {
	t.Helper()
	return newStackOver(t, "../data")
}

// newStackOver runs the stack over the game data in data.
func newStackOver(t *testing.T, data string) *stack {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db := dbx.Config{URL: cluster.URL(t), MaxConns: 4}
	item := runService(t, func(at listen) itemconfig.Config {
		return itemconfig.Config{GRPC: at.grpc(), Ops: at.ops(), ShutdownGrace: grace, DataDir: data, Database: db,
			IdempotencyKeys: itemconfig.IdempotencyKeys{Keep: 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 1000},
			Outbox:          itemconfig.Outbox{PublishedKeep: 24 * time.Hour, UnpublishedKeep: 7 * 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 1000}}
	}, itemapp.Build)
	account := runService(t, func(at listen) accountconfig.Config {
		return accountconfig.Config{GRPC: at.grpc(), Ops: at.ops(), ShutdownGrace: grace, Database: db, Password: cheapPasswords,
			Sessions: accountconfig.Sessions{Keep: 30 * 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 1000}}
	}, accountapp.Build)
	character := runService(t, func(at listen) characterconfig.Config {
		return characterconfig.Config{GRPC: at.grpc(), Ops: at.ops(), ShutdownGrace: grace, DataDir: data, Database: db,
			Dependencies: characterconfig.Dependencies{Item: item.addr}}
	}, characterapp.Build)
	social := runService(t, func(at listen) socialconfig.Config {
		return socialconfig.Config{GRPC: at.grpc(), Ops: at.ops(), ShutdownGrace: grace, DataDir: data, Members: socialconfig.Members{Keep: time.Hour, PruneEvery: time.Hour, PruneBatch: 1000}}
	}, socialapp.Build)
	world := runService(t, func(at listen) worldconfig.Config {
		return worldconfig.Config{GRPC: at.grpc(), Ops: at.ops(), ShutdownGrace: grace, DataDir: data,
			TickRateHz: 20, CheckpointInterval: 30 * time.Second, FlushTimeout: 10 * time.Second, ItemCalls: worldconfig.ItemCalls{Workers: 2, BatchMax: 32},
			Dependencies: worldconfig.Dependencies{Character: character.addr, Item: item.addr, Social: social.addr}}
	}, worldapp.Build)
	itemConn, err := grpcx.Dial(item.addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = itemConn.Close() })
	log := runnertest.NewLog()
	srv, err := app.Build(ctx, gatewayconfig.Config{
		HTTP:          gatewayconfig.HTTP{Addr: "127.0.0.1:0", CORSOrigins: []string{"http://localhost:5173"}},
		Ops:           host.OpsSection{Addr: "127.0.0.1:0"},
		WS:            gatewayconfig.WS{HelloTimeout: 5 * time.Second, CommandQueue: 64},
		Sessions:      gatewayconfig.Sessions{JoinTimeout: 5 * time.Second, CheckInterval: time.Minute},
		ShutdownGrace: grace,
		Dependencies: gatewayconfig.Dependencies{
			Account: account.addr, Character: character.addr, Item: item.addr, World: world.addr, Social: social.addr,
		},
	}, log.Logger(), prometheus.NewRegistry())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	gateway := runnertest.Start(t, log, srv.Run, "http", "ops")
	pool, err := pgxpool.New(ctx, db.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &stack{t: t, url: "http://" + gateway.Addr("http"), ops: "http://" + gateway.Addr("ops"), db: pool, items: itemv1.NewItemServiceClient(itemConn), social: social, world: world, data: data}
}

// listen is where a service's listeners bind, the same addresses on a restart.
type listen struct{ grpcAddr, opsAddr string }

// freeAddrs are n loopback addresses outside the kernel's ephemeral range, so a restart can bind them again.
func freeAddrs(t *testing.T, n int) []string {
	t.Helper()
	var addrs []string
	for _, port := range freeport.GetN(t, n) {
		addrs = append(addrs, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	}
	return addrs
}

func (l listen) grpc() host.GRPCSection { return host.GRPCSection{Listen: l.grpcAddr} }
func (l listen) ops() host.OpsSection   { return host.OpsSection{Addr: l.opsAddr} }

type service struct {
	t     *testing.T
	build func(ctx context.Context, at listen, log *slog.Logger) (*host.Host, error)
	at    listen // where it listens once started
	addr  string // its gRPC address
	stop  func()
}

// runService starts a service on free ports until the test ends.
func runService[C any](t *testing.T, cfg func(listen) C, build func(ctx context.Context, cfg C, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error)) *service {
	t.Helper()
	svc := &service{t: t, build: func(ctx context.Context, at listen, log *slog.Logger) (*host.Host, error) {
		return build(ctx, cfg(at), log, prometheus.NewRegistry())
	}}
	at := freeAddrs(t, 2)
	svc.start(listen{grpcAddr: at[0], opsAddr: at[1]})
	t.Cleanup(func() { svc.stop() })
	return svc
}

func (s *service) start(at listen) {
	s.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	log := runnertest.NewLog()
	h, err := s.build(ctx, at, log.Logger())
	if err != nil {
		s.t.Fatal(err)
	}
	p := runnertest.Start(s.t, log, h.Run, "grpc", "ops")
	s.at, s.addr = at, at.grpcAddr
	s.stop = func() {
		if err := p.Stop(); err != nil {
			s.t.Errorf("service stopped with %v", err)
		}
		h.Close()
		s.stop = func() {}
	}
}

// restart replaces the service with a fresh process on the same addresses, losing its memory.
func (s *service) restart() {
	s.t.Helper()
	s.stop()
	s.start(s.at)
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
