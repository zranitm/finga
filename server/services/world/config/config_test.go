package config_test

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"finga/internal/host"
	"finga/pkg/configload"
	"finga/pkg/logger"
	"finga/services/world/config"
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
	want := config.Config{GRPC: host.GRPCSection{Listen: ":9004"}, Ops: host.OpsSection{Addr: ":9104"}, ShutdownGrace: 10 * time.Second, Log: logger.Config{Level: "info", Format: "json", Sampling: logger.Sampling{Initial: 100, Thereafter: 100}}, DataDir: "data", TickRateHz: 20, CheckpointInterval: 30 * time.Second, FlushTimeout: 10 * time.Second, ItemCalls: config.ItemCalls{Workers: 8, BatchMax: 32}, Dependencies: config.Dependencies{Character: "localhost:9002", Item: "localhost:9003", Social: "localhost:9005"}}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

// Each file holds only its own service's keys.
func TestAKeyOfAnotherServiceIsRefused(t *testing.T) {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data) + "database:\n  url: postgres://finga@localhost/finga\n  max_conns: 1\n"
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

func TestTheTickIntervalFollowsFromTheRate(t *testing.T) {
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.TickInterval(); got != 50*time.Millisecond {
		t.Fatalf("tick interval %v", got)
	}
}
