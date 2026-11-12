package config_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"finga/internal/host"
	"finga/pkg/configload"
	"finga/pkg/observability/logger"
	"finga/services/gateway/config"
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
	want := config.Config{HTTP: config.HTTP{Addr: ":8080", CORSOrigins: []string{}}, Ops: host.OpsSection{Addr: ":9100"},
		WS: config.WS{HelloTimeout: 5 * time.Second, CommandQueue: 64}, Sessions: config.Sessions{JoinTimeout: 5 * time.Second, CheckInterval: time.Minute},
		ShutdownGrace: 10 * time.Second, Log: logger.Config{Level: "info", Format: "json", Sampling: logger.Sampling{Initial: 100, Thereafter: 100}}, Dependencies: config.Dependencies{Account: "localhost:9001", Character: "localhost:9002", Item: "localhost:9003", World: "localhost:9004", Social: "localhost:9005"}}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

func TestABrowserOriginMustBeAURL(t *testing.T) {
	t.Setenv("FINGA_HTTP_CORS_ORIGINS", "http://localhost:4173,not a url")
	if _, err := load(); err == nil || !strings.Contains(err.Error(), "cors_origins") {
		t.Fatalf("err %v", err)
	}
}
