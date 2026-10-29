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
	"finga/services/character/config"
)

// load reads the service's committed file, as the binary does from server/.
func load() (*config.Config, error) {
	return configload.Load[config.Config](configload.Options{Path: "config.yaml", EnvPrefix: host.EnvPrefix, Strict: true})
}

func TestTheDefaultsLoad(t *testing.T) {
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{GRPC: host.GRPCSection{Listen: ":9002"}, Ops: host.OpsSection{Addr: ":9102"}, ShutdownGrace: 10 * time.Second, LogLevel: "info", DataDir: "data", Database: dbx.Config{URL: "postgres://finga:finga@localhost:5432/finga?sslmode=disable", MaxConns: 20}, Dependencies: config.Dependencies{Item: "localhost:9003"}}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

// The strict loader refuses the world service's tick rate: each file holds only its
// own service's keys.
func TestAKeyOfAnotherServiceIsRefused(t *testing.T) {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data) + "tick_rate_hz: 20\n"
	if _, err := configload.LoadFrom[config.Config](strings.NewReader(text), "config.yaml", configload.Options{EnvPrefix: host.EnvPrefix, Strict: true}); err == nil {
		t.Fatal("loaded a key the service does not have")
	}
}

func TestAnOverrideReachesTheServicesKey(t *testing.T) {
	t.Setenv("FINGA_DEPENDENCIES_ITEM", "item:9003")
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Dependencies.Item != "item:9003" {
		t.Fatalf("FINGA_DEPENDENCIES_ITEM gave %q", cfg.Dependencies.Item)
	}
}

func TestTheDatabaseMustBePostgres(t *testing.T) {
	t.Setenv("FINGA_DATABASE_URL", "mysql://finga@localhost/finga")
	if _, err := load(); err == nil || !strings.Contains(err.Error(), "database.url") {
		t.Fatalf("err %v", err)
	}
}
