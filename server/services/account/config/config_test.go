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
	"finga/services/account/config"
)

func load() (*config.Config, error) {
	return configload.Load[config.Config](configload.Options{Path: "config.yaml", EnvPrefix: host.EnvPrefix, Strict: true})
}

func TestTheDefaultsLoad(t *testing.T) {
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{GRPC: host.GRPCSection{Listen: ":9001"}, Ops: host.OpsSection{Addr: ":9101"}, ShutdownGrace: 10 * time.Second, Log: logger.Config{Level: "info", Format: "json", Sampling: logger.Sampling{Initial: 100, Thereafter: 100}}, Database: dbx.Config{URL: "postgres://finga:finga@localhost:5432/finga?sslmode=disable", MaxConns: 20, StatementTimeout: 5 * time.Second, LockTimeout: 2 * time.Second, IdleInTransactionTimeout: 10 * time.Second},
		Password: config.Password{Time: 1, MemoryKiB: 64 * 1024, Threads: 4, Concurrent: 4, Wait: time.Second}}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

func TestAKeyOfAnotherServiceIsRefused(t *testing.T) {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data) + "dependencies:\n  item: localhost:9003\n"
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
