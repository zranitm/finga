package config_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"finga/internal/host"
	"finga/pkg/configload"
	"finga/pkg/dbx"
	"finga/pkg/observability/logger"
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
		IdempotencyKeys: config.IdempotencyKeys{Keep: 24 * time.Hour, PruneEvery: 10 * time.Minute, PruneBatch: 1000, PruneGap: 100 * time.Millisecond},
		Outbox:          config.Outbox{PublishedKeep: 24 * time.Hour, UnpublishedKeep: 7 * 24 * time.Hour, PruneEvery: 10 * time.Minute, PruneBatch: 1000, PruneGap: 100 * time.Millisecond}}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
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

// The unpublished TTL is off at 0, for Stage 2's relay; a published row always goes in time.
func TestTheOutboxsUnpublishedTTLCanBeOff(t *testing.T) {
	for key, valid := range map[string]bool{"UNPUBLISHED_KEEP=0": true, "UNPUBLISHED_KEEP=-1h": false, "PUBLISHED_KEEP=0": false} {
		t.Run(key, func(t *testing.T) {
			name, value, _ := strings.Cut(key, "=")
			t.Setenv("FINGA_OUTBOX_"+name, value)
			if _, err := load(); (err == nil) != valid {
				t.Fatalf("%s: %v", key, err)
			}
		})
	}
}
