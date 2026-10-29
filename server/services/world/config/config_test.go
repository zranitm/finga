package config_test

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"finga/internal/host"
	"finga/pkg/configload"
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
	want := config.Config{GRPC: host.GRPCSection{Listen: ":9004"}, Ops: host.OpsSection{Addr: ":9104"}, ShutdownGrace: 10 * time.Second, LogLevel: "info", DataDir: "data", TickRateHz: 20, CheckpointInterval: 30 * time.Second, Economy: config.Economy{Workers: 8}, Dependencies: config.Dependencies{Character: "localhost:9002", Item: "localhost:9003", Social: "localhost:9005"}}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

// The strict loader refuses a database, which the world service has not: each file holds only its
// own service's keys.
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

func TestTheTickRateMustDivideSixty(t *testing.T) {
	t.Setenv("FINGA_TICK_RATE_HZ", "25")
	if _, err := load(); err == nil || !strings.Contains(err.Error(), "tick_rate_hz") {
		t.Fatalf("err %v", err)
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
