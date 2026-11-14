package config_test

import (
	"reflect"
	"testing"

	"finga/cmd/testers/config"
	"finga/internal/host"
	"finga/pkg/configload"
)

func load() (*config.Config, error) {
	return configload.Load[config.Config](configload.Options{Path: "config.yaml", EnvPrefix: host.EnvPrefix, Strict: true})
}

func TestTheDefaultsLoad(t *testing.T) {
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{DataDir: "data", Gateway: "http://localhost:8080", Admin: "http://localhost:9200", Password: "testers-only"}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

func TestTheComposeNetworkOverridesTheAddresses(t *testing.T) {
	t.Setenv("FINGA_GATEWAY", "http://finga:8080")
	t.Setenv("FINGA_ADMIN", "http://finga:9200")
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gateway != "http://finga:8080" || cfg.Admin != "http://finga:9200" {
		t.Fatalf("got %+v", *cfg)
	}
}
