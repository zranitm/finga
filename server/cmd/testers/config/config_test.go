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
	want := config.Config{DataDir: "data", Gateway: "http://localhost:8080", Password: "testers-only",
		Dependencies: config.Dependencies{Account: "localhost:9001", Character: "localhost:9002", Item: "localhost:9003"}}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

func TestTheComposeNetworkOverridesTheAddresses(t *testing.T) {
	t.Setenv("FINGA_GATEWAY", "http://gateway:8080")
	t.Setenv("FINGA_DEPENDENCIES_ITEM", "item:9003")
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Gateway != "http://gateway:8080" || cfg.Dependencies.Item != "item:9003" {
		t.Fatalf("got %+v", *cfg)
	}
}
