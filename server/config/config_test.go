package config_test

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"finga/config"
	"finga/internal/host"
	"finga/pkg/configload"
	"finga/pkg/dbx"
	"finga/pkg/observability/logger"
	accountconfig "finga/services/account/config"
	characterconfig "finga/services/character/config"
	gatewayconfig "finga/services/gateway/config"
	itemconfig "finga/services/item/config"
	socialconfig "finga/services/social/config"
	worldconfig "finga/services/world/config"
)

// load reads the committed file, as every binary does from server/.
func load() (*config.Config, error) {
	return configload.Load[config.Config](configload.Options{Path: "config.yaml", EnvPrefix: host.EnvPrefix, Strict: true})
}

var database = dbx.Config{URL: "postgres://finga:finga@localhost:5432/finga?sslmode=disable", MaxConns: 60, StatementTimeout: 5 * time.Second, LockTimeout: 2 * time.Second, IdleInTransactionTimeout: 10 * time.Second}

func TestTheDefaultsLoad(t *testing.T) {
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{
		HTTP:          host.HTTPSection{Addr: ":8080", CORSOrigins: []string{}},
		Ops:           host.OpsSection{Addr: ":9100"},
		Admin:         config.AdminSection{Addr: "127.0.0.1:9200"},
		ShutdownGrace: 10 * time.Second,
		Log:           logger.Config{Level: "info", Format: "json", Sampling: logger.Sampling{Initial: 100, Thereafter: 100}},
		Database:      database,
		DataDir:       "data",
		Gateway: gatewayconfig.Config{Ops: host.OpsSection{Addr: ":9100"},
			WS: gatewayconfig.WS{HelloTimeout: 5 * time.Second, CommandQueue: 64}, Sessions: gatewayconfig.Sessions{JoinTimeout: 5 * time.Second, CheckInterval: time.Minute},
			Dependencies: gatewayconfig.Dependencies{Account: "localhost:9001", Character: "localhost:9002", Item: "localhost:9003", World: "localhost:9004", Social: "localhost:9005"}},
		Account: accountconfig.Config{GRPC: host.GRPCSection{Listen: ":9001"}, Ops: host.OpsSection{Addr: ":9101"},
			Password: accountconfig.Password{Time: 1, MemoryKiB: 64 * 1024, Threads: 4, Concurrent: 4, Wait: time.Second},
			Sessions: accountconfig.Sessions{Keep: 30 * 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 1000, PruneGap: 100 * time.Millisecond}},
		Character: characterconfig.Config{GRPC: host.GRPCSection{Listen: ":9002"}, Ops: host.OpsSection{Addr: ":9102"}, Dependencies: characterconfig.Dependencies{Item: "localhost:9003"}},
		Item: itemconfig.Config{GRPC: host.GRPCSection{Listen: ":9003"}, Ops: host.OpsSection{Addr: ":9103"},
			IdempotencyKeys: itemconfig.IdempotencyKeys{Keep: 24 * time.Hour, PruneEvery: 10 * time.Minute, PruneBatch: 1000, PruneGap: 100 * time.Millisecond},
			Outbox:          itemconfig.Outbox{PublishedKeep: 24 * time.Hour, UnpublishedKeep: 7 * 24 * time.Hour, PruneEvery: 10 * time.Minute, PruneBatch: 1000, PruneGap: 100 * time.Millisecond}},
		World: worldconfig.Config{GRPC: host.GRPCSection{Listen: ":9004"}, Ops: host.OpsSection{Addr: ":9104"},
			TickRateHz: 20, CheckpointInterval: 30 * time.Second, FlushTimeout: 10 * time.Second, ItemCalls: worldconfig.ItemCalls{Workers: 8, BatchMax: 32},
			Dependencies: worldconfig.Dependencies{Character: "localhost:9002", Item: "localhost:9003", Social: "localhost:9005"}},
		Social: socialconfig.Config{GRPC: host.GRPCSection{Listen: ":9005"}, Ops: host.OpsSection{Addr: ":9105"},
			Members: socialconfig.Members{Keep: time.Hour, PruneEvery: 5 * time.Minute, PruneBatch: 1000, PruneGap: 100 * time.Millisecond}},
	}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

// Each module's process gets the shared sections and its own addresses; the gateway and cmd/finga serve no gRPC.
func TestEachProcessGetsItsOwnAddresses(t *testing.T) {
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	for process, want := range map[string][2]string{
		"finga": {"", ":9100"}, "gateway": {"", ":9100"}, "account": {":9001", ":9101"}, "character": {":9002", ":9102"},
		"item": {":9003", ":9103"}, "world": {":9004", ":9104"}, "social": {":9005", ":9105"},
	} {
		s, err := cfg.Host(process)
		if err != nil || s.GRPC != want[0] || s.Ops != want[1] || s.DataDir != "data" || s.Database != database || s.ShutdownGrace != 10*time.Second {
			t.Errorf("%s: %+v, %v", process, s, err)
		}
	}
	if _, err := cfg.Host("auction"); err == nil {
		t.Fatal("settings for a process no module names")
	}
}

// An override names the module's section: FINGA_<MODULE>_<KEY>.
func TestAnOverrideReachesItsModulesSection(t *testing.T) {
	t.Setenv("FINGA_WORLD_DEPENDENCIES_ITEM", "item:9003")
	t.Setenv("FINGA_WORLD_ITEM_CALLS_WORKERS", "2")
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.World.Dependencies.Item != "item:9003" || cfg.World.ItemCalls.Workers != 2 || cfg.Character.Dependencies.Item != "localhost:9003" {
		t.Fatalf("world %+v, character %+v", cfg.World, cfg.Character)
	}
}

func TestABrowserOriginMustBeAURL(t *testing.T) {
	t.Setenv("FINGA_HTTP_CORS_ORIGINS", "http://localhost:4173,not a url")
	if _, err := load(); err == nil || !strings.Contains(err.Error(), "cors_origins") {
		t.Fatalf("err %v", err)
	}
}

// A key kept for less than an hour could expire inside a caller's retries, running the command twice.
func TestAKeysRetentionIsAtLeastAnHour(t *testing.T) {
	for keep, valid := range map[string]bool{"1m": false, "59m": false, "1h": true, "24h": true} {
		t.Run(keep, func(t *testing.T) {
			t.Setenv("FINGA_ITEM_IDEMPOTENCY_KEYS_KEEP", keep)
			if _, err := load(); (err == nil) != valid {
				t.Fatalf("keep %s: %v", keep, err)
			}
		})
	}
}

// The unpublished TTL is off at 0, for Stage 2's relay; a published row always goes in time.
func TestTheOutboxsUnpublishedTTLCanBeOff(t *testing.T) {
	for key, valid := range map[string]bool{"UNPUBLISHED_KEEP=0": true, "UNPUBLISHED_KEEP=-1h": false, "PUBLISHED_KEEP=0": false} {
		t.Run(key, func(t *testing.T) {
			name, value, _ := strings.Cut(key, "=")
			t.Setenv("FINGA_ITEM_OUTBOX_"+name, value)
			if _, err := load(); (err == nil) != valid {
				t.Fatalf("%s: %v", key, err)
			}
		})
	}
}

// A gateway back within its 30 s grace re-registers its players from what social kept, so keep never drops below a few minutes.
func TestMembersAreKeptAFewMinutesAtLeast(t *testing.T) {
	for keep, valid := range map[string]bool{"30s": false, "4m": false, "5m": true, "1h": true} {
		t.Run(keep, func(t *testing.T) {
			t.Setenv("FINGA_SOCIAL_MEMBERS_KEEP", keep)
			if _, err := load(); (err == nil) != valid {
				t.Fatalf("keep %s: %v", keep, err)
			}
		})
	}
}

// Compose waits out the game's shutdown before SIGKILL, world's last saves included, in dev and in prod; what is left over is the trace flush and a margin.
func TestComposeWaitsOutEachShutdown(t *testing.T) {
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	grace := map[string]string{}
	for _, file := range []string{"../../infra/docker-compose.yml", "../../infra/deploy/compose.prod.yml"} { // prod's overlay overrides the base
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var compose struct {
			Services map[string]struct {
				StopGrace string `yaml:"stop_grace_period"`
			} `yaml:"services"`
		}
		if err := yaml.Unmarshal(raw, &compose); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		for name, s := range compose.Services {
			if s.StopGrace != "" {
				grace[name] = s.StopGrace
			}
		}
	}
	need := cfg.ShutdownGrace + cfg.World.FlushTimeout
	got, err := time.ParseDuration(grace["finga"])
	if err != nil {
		t.Fatalf("finga: stop_grace_period %q: %v", grace["finga"], err)
	}
	if got <= need {
		t.Errorf("finga: stop_grace_period %s, but its shutdown takes up to %s", got, need)
	}
}
