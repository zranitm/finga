package config_test

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"finga/internal/host"
	"finga/pkg/configload"
	"finga/pkg/dbx"
	"finga/pkg/logger"
	"finga/services/item/config"
)

func load() (*config.Config, error) {
	return configload.Load[config.Config](configload.Options{Path: "config.yaml", EnvPrefix: host.EnvPrefix, Strict: true})
}

func TestTheDefaultsLoad(t *testing.T) {
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{GRPC: host.GRPCSection{Listen: ":9003"}, Ops: host.OpsSection{Addr: ":9103"}, ShutdownGrace: 10 * time.Second, Log: logger.Config{Level: "info", Format: "json", Sampling: logger.Sampling{Initial: 100, Thereafter: 100}}, DataDir: "data", Database: dbx.Config{URL: "postgres://finga:finga@localhost:5432/finga?sslmode=disable", MaxConns: 20, StatementTimeout: 5 * time.Second, LockTimeout: 2 * time.Second, IdleInTransactionTimeout: 10 * time.Second},
		IdempotencyKeys: config.IdempotencyKeys{Keep: 24 * time.Hour, PruneEvery: 10 * time.Minute, PruneBatch: 1000}}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

func TestAKeyOfAnotherServiceIsRefused(t *testing.T) {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data) + "dependencies:\n  social: localhost:9005\n"
	if _, err := configload.LoadFrom[config.Config](strings.NewReader(text), "config.yaml", configload.Options{EnvPrefix: host.EnvPrefix, Strict: true}); err == nil {
		t.Fatal("loaded a key the service does not have")
	}
}

func TestAnOverrideReachesTheServicesKey(t *testing.T) {
	t.Setenv("FINGA_DATABASE_URL", "postgres://other@db:5432/finga")
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.URL != "postgres://other@db:5432/finga" {
		t.Fatalf("FINGA_DATABASE_URL gave %q", cfg.Database.URL)
	}
}

func TestTheDatabaseMustBePostgres(t *testing.T) {
	t.Setenv("FINGA_DATABASE_URL", "mysql://finga@localhost/finga")
	if _, err := load(); err == nil || !strings.Contains(err.Error(), "database.url") {
		t.Fatalf("err %v", err)
	}
}

// A key kept for less than an hour could expire inside a caller's retries, running the command twice.
func TestAKeysRetentionIsAtLeastAnHour(t *testing.T) {
	for keep, valid := range map[string]bool{"1m": false, "59m": false, "1h": true, "24h": true} {
		t.Run(keep, func(t *testing.T) {
			t.Setenv("FINGA_IDEMPOTENCY_KEYS_KEEP", keep)
			if _, err := load(); (err == nil) != valid {
				t.Fatalf("keep %s: %v", keep, err)
			}
		})
	}
}
