package config_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"finga/cmd/migrate/config"
	"finga/internal/host"
	"finga/pkg/configload"
	"finga/pkg/dbx"
)

// load reads the command's committed file, as the binary does from server/.
func load() (*config.Config, error) {
	return configload.Load[config.Config](configload.Options{Path: "config.yaml", EnvPrefix: host.EnvPrefix, Strict: true})
}

func TestTheDefaultsLoad(t *testing.T) {
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	want := config.Config{LogLevel: "info", Database: dbx.Config{URL: "postgres://finga:finga@localhost:5432/finga?sslmode=disable", MaxConns: 2}}
	if !reflect.DeepEqual(*cfg, want) {
		t.Fatalf("got  %+v\nwant %+v", *cfg, want)
	}
}

// The strict loader refuses a service's key: migrate has no listener.
func TestAKeyOfAServiceIsRefused(t *testing.T) {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data) + "grpc:\n  listen: localhost:9000\n"
	if _, err := configload.LoadFrom[config.Config](strings.NewReader(text), "config.yaml", configload.Options{EnvPrefix: host.EnvPrefix, Strict: true}); err == nil {
		t.Fatal("loaded a key the command does not have")
	}
}

func TestTheDatabaseURLIsOverridden(t *testing.T) {
	t.Setenv("FINGA_DATABASE_URL", "postgres://other@db:5432/finga")
	cfg, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.URL != "postgres://other@db:5432/finga" {
		t.Fatalf("url %q", cfg.Database.URL)
	}
}
