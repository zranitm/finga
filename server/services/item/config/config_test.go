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
	"finga/services/item/config"
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
	want := config.Config{GRPC: host.GRPCSection{Listen: ":9003"}, Ops: host.OpsSection{Addr: ":9103"}, ShutdownGrace: 10 * time.Second, LogLevel: "info", DataDir: "data", Database: dbx.Config{URL: "postgres://finga:finga@localhost:5432/finga?sslmode=disable", MaxConns: 20}}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

// The strict loader refuses a dependencies section, which the item service has not: each file holds only its
// own service's keys.
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
