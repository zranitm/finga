// Package config is the configuration schema every binary shares (Low-level
// design, pages 1 and 10): sections with tags, plus the cross-field rules
// tags can't express. Defaults live in configs/config.yaml; every key can be
// overridden as FINGA_ plus the key path in upper case. Each binary reads
// the sections it needs.
package config

import (
	"errors"
	"net/url"
	"time"
)

// EnvPrefix is the prefix of every environment override.
const EnvPrefix = "FINGA"

// subStepsPerSecond is the movement simulation's fixed sub-step rate
// (page 5): a tick must hold a whole number of 1/60 s sub-steps.
const subStepsPerSecond = 60

// Config is the whole schema.
type Config struct {
	Server    Server    `mapstructure:"server"`
	Database  Database  `mapstructure:"database"`
	World     World     `mapstructure:"world"`
	Economy   Economy   `mapstructure:"economy"`
	Telemetry Telemetry `mapstructure:"telemetry"`
	Services  Services  `mapstructure:"services"`
}

// Server is the HTTP listener for /api, /ws, the probes, and /metrics.
type Server struct {
	Addr          string        `mapstructure:"addr" validate:"required"`
	CORSOrigins   []string      `mapstructure:"cors_origins" validate:"dive,url"` // empty: same origin only
	ShutdownGrace time.Duration `mapstructure:"shutdown_grace" validate:"gt=0"`
}

// Database is the Postgres connection.
type Database struct {
	URL      string `mapstructure:"url" validate:"required"`
	MaxConns int32  `mapstructure:"max_conns" validate:"min=1"`
}

// World is the map instances' tick and persistence.
type World struct {
	TickRateHz         int           `mapstructure:"tick_rate_hz" validate:"min=1,max=60"`
	CheckpointInterval time.Duration `mapstructure:"checkpoint_interval" validate:"gt=0"` // TD-02
	DataDir            string        `mapstructure:"data_dir" validate:"required"`
}

// TickInterval is the time between ticks.
func (w World) TickInterval() time.Duration {
	return time.Second / time.Duration(w.TickRateHz)
}

// Economy is the pool of workers that run economic transactions.
type Economy struct {
	Workers int `mapstructure:"workers" validate:"min=1"`
}

// Services are the internal services' addresses (Low-level design, page
// 10). Stage 1 runs one node of each.
type Services struct {
	Account   Endpoint `mapstructure:"account"`
	Character Endpoint `mapstructure:"character"`
	Item      Endpoint `mapstructure:"item"`
	World     Endpoint `mapstructure:"world"`
	Social    Endpoint `mapstructure:"social"`
}

// Endpoint is where one service listens and is reached.
type Endpoint struct {
	Listen string `mapstructure:"listen" validate:"required"` // the service's gRPC listener
	Addr   string `mapstructure:"addr" validate:"required"`   // where callers dial it; Compose names the service
	Ops    string `mapstructure:"ops" validate:"required"`    // the service's /healthz, /readyz and /metrics
}

// Telemetry is logging.
type Telemetry struct {
	LogLevel string `mapstructure:"log_level" validate:"oneof=debug info warn error"`
}

// Validate holds the rules tags can't express: the database URL is a
// Postgres URL, and the tick rate divides the sub-step rate. Messages name
// the key and never echo the value.
func (c *Config) Validate() error {
	u, err := url.Parse(c.Database.URL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		return errors.New("database.url must be a postgres:// or postgresql:// URL")
	}
	if subStepsPerSecond%c.World.TickRateHz != 0 {
		return errors.New("world.tick_rate_hz must divide 60, so each tick holds whole 1/60 s sub-steps")
	}
	return nil
}
