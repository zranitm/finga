package config_test

import (
	"strings"
	"testing"
	"time"

	"finga/internal/config"
	"finga/pkg/configload"
)

const defaultsPath = "../../configs/config.yaml"

func loadDefaults(t *testing.T) (*config.Config, error) {
	t.Helper()
	return configload.Load[config.Config](configload.Options{Path: defaultsPath, EnvPrefix: config.EnvPrefix, Strict: true})
}

func loadText(t *testing.T, yaml string) (*config.Config, error) {
	t.Helper()
	return configload.LoadFrom[config.Config](strings.NewReader(yaml), "test.yaml", configload.Options{EnvPrefix: config.EnvPrefix, Strict: true})
}

func TestDefaultsLoad(t *testing.T) {
	cfg, err := loadDefaults(t)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		got  any
		want any
	}{
		{"server.addr", cfg.Server.Addr, ":8080"},
		{"server.cors_origins is empty", len(cfg.Server.CORSOrigins), 0},
		{"server.shutdown_grace", cfg.Server.ShutdownGrace, 10 * time.Second},
		{"database.url", cfg.Database.URL, "postgres://finga:finga@localhost:5432/finga?sslmode=disable"},
		{"database.max_conns", cfg.Database.MaxConns, int32(20)},
		{"world.tick_rate_hz", cfg.World.TickRateHz, 20},
		{"world.checkpoint_interval", cfg.World.CheckpointInterval, 30 * time.Second},
		{"world.data_dir", cfg.World.DataDir, "data"},
		{"economy.workers", cfg.Economy.Workers, 8},
		{"telemetry.log_level", cfg.Telemetry.LogLevel, "info"},
		{"the tick interval follows from the rate", cfg.World.TickInterval(), 50 * time.Millisecond},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

// valid is a complete, valid document; each rejection row breaks one key.
const valid = `
server:
  addr: ":8080"
  cors_origins: ["http://localhost:5173"]
  shutdown_grace: 10s
database:
  url: postgres://finga:finga@localhost:5432/finga?sslmode=disable
  max_conns: 20
world:
  tick_rate_hz: 20
  checkpoint_interval: 30s
  data_dir: data
economy:
  workers: 8
telemetry:
  log_level: info
`

func TestValidationNamesTheKey(t *testing.T) {
	tests := []struct {
		name    string
		old     string
		replace string
		wantKey string
	}{
		{"server.addr required", `addr: ":8080"`, `addr: ""`, "server.addr"},
		{"server.cors_origins must be URLs", `["http://localhost:5173"]`, `["localhost"]`, "server.cors_origins"},
		{"server.shutdown_grace positive", `shutdown_grace: 10s`, `shutdown_grace: 0s`, "server.shutdown_grace"},
		{"database.url required", `url: postgres://finga:finga@localhost:5432/finga?sslmode=disable`, `url: ""`, "database.url"},
		{"database.url must be a postgres URL", `url: postgres://finga:finga@localhost:5432/finga?sslmode=disable`, `url: mysql://localhost/finga`, "database.url"},
		{"database.max_conns at least 1", `max_conns: 20`, `max_conns: 0`, "database.max_conns"},
		{"world.tick_rate_hz at least 1", `tick_rate_hz: 20`, `tick_rate_hz: 0`, "world.tick_rate_hz"},
		{"world.tick_rate_hz at most 60", `tick_rate_hz: 20`, `tick_rate_hz: 120`, "world.tick_rate_hz"},
		{"world.tick_rate_hz divides 60, so a tick holds whole 1/60 s sub-steps", `tick_rate_hz: 20`, `tick_rate_hz: 25`, "world.tick_rate_hz"},
		{"world.checkpoint_interval positive", `checkpoint_interval: 30s`, `checkpoint_interval: 0s`, "world.checkpoint_interval"},
		{"world.data_dir required", `data_dir: data`, `data_dir: ""`, "world.data_dir"},
		{"economy.workers at least 1", `workers: 8`, `workers: 0`, "economy.workers"},
		{"telemetry.log_level is one of the four", `log_level: info`, `log_level: verbose`, "telemetry.log_level"},
		{"an unknown key is rejected", `log_level: info`, "log_level: info\n  colour: true", "colour"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc := strings.Replace(valid, tt.old, tt.replace, 1)
			if doc == valid {
				t.Fatalf("replacement %q not found in the document", tt.old)
			}
			_, err := loadText(t, doc)
			if err == nil {
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), tt.wantKey) {
				t.Fatalf("error %q does not name %q", err, tt.wantKey)
			}
		})
	}
}

func TestValidDocumentLoads(t *testing.T) {
	cfg, err := loadText(t, valid)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.CORSOrigins[0] != "http://localhost:5173" {
		t.Fatalf("cors_origins = %v", cfg.Server.CORSOrigins)
	}
}

func TestEnvOverridesTheYAML(t *testing.T) {
	tests := []struct {
		name  string
		key   string
		value string
		get   func(*config.Config) any
		want  any
	}{
		{"FINGA_WORLD_TICK_RATE_HZ", "FINGA_WORLD_TICK_RATE_HZ", "30", func(c *config.Config) any { return c.World.TickRateHz }, 30},
		{"FINGA_SERVER_ADDR", "FINGA_SERVER_ADDR", ":9090", func(c *config.Config) any { return c.Server.Addr }, ":9090"},
		{"FINGA_SERVER_CORS_ORIGINS as a list", "FINGA_SERVER_CORS_ORIGINS", "http://a:1,http://b:2", func(c *config.Config) any { return len(c.Server.CORSOrigins) }, 2},
		{"FINGA_DATABASE_MAX_CONNS", "FINGA_DATABASE_MAX_CONNS", "5", func(c *config.Config) any { return c.Database.MaxConns }, int32(5)},
		{"FINGA_TELEMETRY_LOG_LEVEL", "FINGA_TELEMETRY_LOG_LEVEL", "debug", func(c *config.Config) any { return c.Telemetry.LogLevel }, "debug"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.key, tt.value)
			cfg, err := loadDefaults(t)
			if err != nil {
				t.Fatal(err)
			}
			if got := tt.get(cfg); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnvOverrideIsValidatedToo(t *testing.T) {
	t.Setenv("FINGA_WORLD_TICK_RATE_HZ", "0")
	_, err := loadDefaults(t)
	if err == nil || !strings.Contains(err.Error(), "world.tick_rate_hz") {
		t.Fatalf("err = %v, want it to name world.tick_rate_hz", err)
	}
}
