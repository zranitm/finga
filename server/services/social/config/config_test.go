package config_test

import (
	"reflect"
	"testing"
	"time"

	"finga/internal/host"
	"finga/pkg/configload"
	"finga/pkg/observability/logger"
	"finga/services/social/config"
)

func load() (*config.Config, error) {
	return configload.Load[config.Config](configload.Options{Path: "config.yaml", EnvPrefix: host.EnvPrefix, Strict: true})
}

func TestTheDefaultsLoad(t *testing.T) {
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{GRPC: host.GRPCSection{Listen: ":9005"}, Ops: host.OpsSection{Addr: ":9105"}, ShutdownGrace: 10 * time.Second, Log: logger.Config{Level: "info", Format: "json", Sampling: logger.Sampling{Initial: 100, Thereafter: 100}}, DataDir: "data", Members: config.Members{Keep: time.Hour, PruneEvery: 5 * time.Minute, PruneBatch: 1000, PruneGap: 100 * time.Millisecond}}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

// A gateway back within its 30 s grace re-registers its players from what social kept, so keep never drops below a few minutes.
func TestMembersAreKeptAFewMinutesAtLeast(t *testing.T) {
	for keep, valid := range map[string]bool{"30s": false, "4m": false, "5m": true, "1h": true} {
		t.Run(keep, func(t *testing.T) {
			t.Setenv("FINGA_MEMBERS_KEEP", keep)
			if _, err := load(); (err == nil) != valid {
				t.Fatalf("keep %s: %v", keep, err)
			}
		})
	}
}
