package config_test

import (
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"finga/internal/host"
	"finga/pkg/configload"
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
	want := config.Config{HTTP: config.HTTP{Addr: ":8080", CORSOrigins: []string{}}, ShutdownGrace: 10 * time.Second, LogLevel: "info", Dependencies: config.Dependencies{Account: "localhost:9001", Character: "localhost:9002", Item: "localhost:9003", World: "localhost:9004", Social: "localhost:9005"}}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

// The strict loader refuses a gRPC listener, which the gateway has not: each file holds only its
// own service's keys.
func TestAKeyOfAnotherServiceIsRefused(t *testing.T) {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data) + "grpc:\n  listen: localhost:9000\n"
	if _, err := configload.LoadFrom[config.Config](strings.NewReader(text), "config.yaml", configload.Options{EnvPrefix: host.EnvPrefix, Strict: true}); err == nil {
		t.Fatal("loaded a key the service does not have")
	}
}

func TestAnOverrideReachesTheServicesKey(t *testing.T) {
	t.Setenv("FINGA_HTTP_ADDR", ":9090")
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP.Addr != ":9090" {
		t.Fatalf("FINGA_HTTP_ADDR gave %q", cfg.HTTP.Addr)
	}
}

func TestABrowserOriginMustBeAURL(t *testing.T) {
	t.Setenv("FINGA_HTTP_CORS_ORIGINS", "http://localhost:4173,not a url")
	if _, err := load(); err == nil || !strings.Contains(err.Error(), "cors_origins") {
		t.Fatalf("err %v", err)
	}
}
