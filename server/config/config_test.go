package config_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"finga/config"
	"finga/internal/host"
	"finga/pkg/configload"
	"finga/pkg/dbx"
	"finga/pkg/observability/logger"
	accountconfig "finga/services/account/config"
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
		Gateway: gatewayconfig.Config{
			WS: gatewayconfig.WS{HelloTimeout: 5 * time.Second, CommandQueue: 64}, Sessions: gatewayconfig.Sessions{JoinTimeout: 5 * time.Second, CheckInterval: time.Minute}},
		Account: accountconfig.Config{
			Password: accountconfig.Password{Time: 1, MemoryKiB: 64 * 1024, Threads: 4, Concurrent: 4, Wait: time.Second},
			Sessions: accountconfig.Sessions{Keep: 30 * 24 * time.Hour, PruneEvery: time.Hour, PruneBatch: 1000, PruneGap: 100 * time.Millisecond}},
		Item: itemconfig.Config{
			IdempotencyKeys: itemconfig.IdempotencyKeys{Keep: 24 * time.Hour, PruneEvery: 10 * time.Minute, PruneBatch: 1000, PruneGap: 100 * time.Millisecond},
			Outbox:          itemconfig.Outbox{PublishedKeep: 24 * time.Hour, UnpublishedKeep: 7 * 24 * time.Hour, PruneEvery: 10 * time.Minute, PruneBatch: 1000, PruneGap: 100 * time.Millisecond}},
		World: worldconfig.Config{
			TickRateHz: 20, CheckpointInterval: 30 * time.Second, FlushTimeout: 10 * time.Second, ItemCalls: worldconfig.ItemCalls{Workers: 8, BatchMax: 32}},
		Social: socialconfig.Config{
			Members: socialconfig.Members{Keep: time.Hour, PruneEvery: 5 * time.Minute, PruneBatch: 1000, PruneGap: 100 * time.Millisecond}},
	}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

// cmd/finga's process gets the shared sections and the ops address; no other process has settings.
func TestOnlyFingaHasSettings(t *testing.T) {
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	s, err := cfg.Host("finga")
	if err != nil || s.Ops != ":9100" || s.DataDir != "data" || s.Database != database || s.ShutdownGrace != 10*time.Second {
		t.Errorf("finga: %+v, %v", s, err)
	}
	for _, process := range []string{"gateway", "world", "auction"} {
		if _, err := cfg.Host(process); err == nil {
			t.Errorf("settings for a process %s", process)
		}
	}
}

// An override names the module's section: FINGA_<MODULE>_<KEY>.
func TestAnOverrideReachesItsModulesSection(t *testing.T) {
	t.Setenv("FINGA_WORLD_ITEM_CALLS_WORKERS", "2")
	t.Setenv("FINGA_ACCOUNT_SESSIONS_PRUNE_BATCH", "50")
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.World.ItemCalls.Workers != 2 || cfg.Account.Sessions.PruneBatch != 50 || cfg.Item.IdempotencyKeys.PruneBatch != 1000 {
		t.Fatalf("world %+v, account %+v, item %+v", cfg.World, cfg.Account, cfg.Item)
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
