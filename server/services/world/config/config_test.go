package config_test

import (
	"reflect"
	"testing"
	"time"

	"finga/internal/host"
	"finga/pkg/configload"
	"finga/pkg/observability/logger"
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

func TestTheTickIntervalFollowsFromTheRate(t *testing.T) {
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.TickInterval(); got != 50*time.Millisecond {
		t.Fatalf("tick interval %v", got)
	}
}
