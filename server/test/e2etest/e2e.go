// Package e2etest is the end-to-end tests' support: the game over a fresh database on loopback ports, its players and their clients, and the checks the tests share; only tests import it.
package e2etest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/sdk/freeport"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	migrateapp "finga/cmd/migrate/app"
	"finga/config"
	"finga/internal/application/item"
	repository "finga/internal/repository/item"
	"finga/internal/shared/gamedata"
	"finga/internal/shared/gamedata/load"
	"finga/internal/shared/host"
	"finga/pkg/dbx"
	"finga/pkg/pgtest"
	"finga/pkg/runner/runnertest"
)

// Cluster is the test binary's Postgres, its template migrated as cmd/migrate migrates; Main starts it.
var Cluster *pgtest.Cluster

// Main runs a test binary's tests over Cluster, for its TestMain to exit with.
func Main(m *testing.M) int {
	ctx := context.Background()
	var err error
	Cluster, err = pgtest.Start(ctx, func(ctx context.Context, pool *pgxpool.Pool) error {
		_, err := migrateapp.Apply(ctx, pool) // the sequence, as cmd/migrate applies it
		return err
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = Cluster.Close(ctx) }()
	return m.Run()
}

// Stack is the game over a fresh database: cmd/finga's app, every module in one process, on loopback ports.
type Stack struct {
	T     *testing.T
	URL   string // the players' listener
	Ops   string // /healthz, /readyz and /metrics
	Admin string // the private admin API, on as in dev
	DB    *pgxpool.Pool
	Items *item.Module // the item module on a host of its own over the same database, for rows that call it directly
	Data  string       // the game data every module loads
	Cfg   config.Config
	build host.Build[config.Config] // cmd/finga's app, or a test's composition
	host  *host.Host                // the game's, as last started
	Stop  func()

	readOnce sync.Once // Data read for a walk, when the game runs as its own process
	read     *gamedata.Data
	readErr  error
}

// gameData is the data the game plays: its host's, or Data read once when the game runs as its own process.
func (s *Stack) gameData() (*gamedata.Data, error) {
	if s.host != nil {
		return s.host.GameData()
	}
	s.readOnce.Do(func() { s.read, s.readErr = load.Dir(s.Data) })
	return s.read, s.readErr
}

const grace = time.Second

// ItemConfig is the item module's, for the game and for the module a row calls directly.
var ItemConfig = config.Item{
	IdempotencyKeys: config.IdempotencyKeys{Keep: 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 1000},
	Outbox:          config.Outbox{PublishedKeep: 24 * time.Hour, UnpublishedKeep: 7 * 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 1000},
}

// cheapPasswords keeps the stack's hashing fast; production's cost is in config/config.yaml.
var cheapPasswords = config.Password{Time: 1, MemoryKiB: 8 * 1024, Threads: 1, Concurrent: 2, Wait: time.Second}

// NewStackBuilt runs the game composed by build over the game data in data, its config as edits change it.
func NewStackBuilt(t *testing.T, data string, build host.Build[config.Config], edits ...func(*config.Config)) *Stack {
	t.Helper()
	t0 := time.Now()
	defer func() {
		fmt.Fprintf(os.Stderr, "E2ETIMING stack %s boot %v, at %v of the process\n", t.Name(), time.Since(t0), time.Since(procStart))
	}()
	at := FreeAddrs(t, 3)
	s := &Stack{T: t, URL: "http://" + at[0], Ops: "http://" + at[1], Admin: "http://" + at[2], Data: data, build: build, Cfg: config.Config{
		HTTP:          config.HTTP{Addr: at[0], CORSOrigins: []string{"http://localhost:5173"}, AccountLimit: config.AccountLimit{PerSecond: 2, Burst: 10}}, // the limit is config.yaml's, which TestTheDefaultsLoad pins
		Ops:           host.OpsSection{Addr: at[1]},
		Admin:         config.AdminSection{Addr: at[2], Enabled: true},
		ShutdownGrace: grace,
		DataDir:       data,
		Database:      dbx.Config{URL: Cluster.URL(t), MaxConns: 12},
		WS:            config.WS{HelloTimeout: 5 * time.Second, CommandQueue: 64},
		Account: config.Account{
			Password:    cheapPasswords,
			Sessions:    config.AccountSessions{Keep: 30 * 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 1000},
			Connections: config.AccountConnections{JoinTimeout: 5 * time.Second, CheckInterval: time.Minute},
		},
		Item:   ItemConfig,
		World:  config.World{TickRateHz: 20, CheckpointInterval: 30 * time.Second, FlushTimeout: 10 * time.Second, ItemCalls: config.ItemCalls{Workers: 2, BatchMax: 32}, Channels: config.Channels{Cap: 150, Idle: 5 * time.Minute}},
		Social: config.Social{Members: config.Members{Keep: time.Hour, PruneEvery: time.Hour, PruneBatch: 1000}},
	}}
	for _, edit := range edits {
		edit(&s.Cfg)
	}
	t1 := time.Now()
	s.start()
	fmt.Fprintf(os.Stderr, "E2ETIMING stack %s start() %v\n", t.Name(), time.Since(t1))
	t.Cleanup(func() { s.Stop() })
	pool, err := pgxpool.New(context.Background(), s.Cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	loaded, err := s.host.GameData() // loaded by the game's start: the item module shares it rather than reading it again
	if err != nil {
		t.Fatal(err)
	}
	s.DB, s.Items = pool, itemsOn(t, s.Cfg, loaded)
	return s
}

// ItemsOver is the item module on a host never run, over cfg's database: what a row asks of it goes through the module's rules, as a world's call does.
func ItemsOver(t *testing.T, cfg config.Config) *item.Module {
	t.Helper()
	return itemsOn(t, cfg, nil)
}

// itemsOn is ItemsOver over game data already loaded from cfg's data directory, or loading it when data is nil.
func itemsOn(t *testing.T, cfg config.Config, data *gamedata.Data) *item.Module {
	t.Helper()
	h, err := host.New(host.Options{Name: "items", Settings: host.Settings{DataDir: cfg.DataDir, Database: cfg.Database}, Logger: runnertest.NewLog().Logger(), Registry: prometheus.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	if data == nil {
		if data, err = h.GameData(); err != nil {
			t.Fatal(err)
		}
	}
	m, err := h.Module("item")
	if err != nil {
		t.Fatal(err)
	}
	pool, dbMetrics, err := m.Database(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	items, err := item.New(item.Deps{Module: m, Config: cfg.Item, Data: data, Pool: pool, Store: repository.NewPostgres(pool, dbMetrics)})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

// FreeAddrs are n loopback addresses outside the kernel's ephemeral range, so a restart can bind them again.
func FreeAddrs(t *testing.T, n int) []string {
	t.Helper()
	var addrs []string
	for _, port := range freeport.GetN(t, n) {
		addrs = append(addrs, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	}
	return addrs
}

// start runs the game until the test ends, on the stack's addresses each time.
func (s *Stack) start() {
	s.T.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // the shipped Willowmere's generation and walk grid take minutes under coverage; stacks run on the stand-ins, bar a smoke test
	defer cancel()
	settings, err := s.Cfg.Host("finga")
	if err != nil {
		s.T.Fatal(err)
	}
	log := runnertest.NewLog()
	h, err := host.New(host.Options{Name: "finga", Settings: settings, Logger: log.Logger(), Registry: prometheus.NewRegistry()})
	if err != nil {
		s.T.Fatal(err)
	}
	if err := s.build(ctx, h, s.Cfg); err != nil {
		h.Close()
		s.T.Fatal(err)
	}
	s.host = h
	p := runnertest.Start(s.T, log, h.Run, "http", "ops")
	s.Stop = func() {
		if err := p.Stop(); err != nil {
			s.T.Errorf("the game stopped with %v", err)
		}
		h.Close()
		s.Stop = func() {}
	}
}

// Restart stops the game as a deploy does, then starts a fresh process on the same addresses: what it held in memory is gone, what it saved stays.
func (s *Stack) Restart() {
	s.T.Helper()
	s.Stop()
	s.start()
}

type response struct {
	Status int
	Body   map[string]any
}

// Call makes an HTTP request of the stack's API, with token if not empty, and decodes its JSON body.
func (s *Stack) Call(method, path, token string, body any) response {
	s.T.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			s.T.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, s.URL+path, &buf)
	if err != nil {
		s.T.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.T.Fatal(err)
	}
	defer res.Body.Close()
	out := response{Status: res.StatusCode}
	raw, _ := io.ReadAll(res.Body)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out.Body); err != nil {
			s.T.Fatalf("%s %s: body %q is not JSON", method, path, raw)
		}
	}
	return out
}

// MustStatus fails the test unless r's status is want.
func (s *Stack) MustStatus(r response, want int) {
	s.T.Helper()
	if r.Status != want {
		s.T.Fatalf("status %d, want %d: %v", r.Status, want, r.Body)
	}
}

// procStart is when the test binary began.
var procStart = time.Now()
