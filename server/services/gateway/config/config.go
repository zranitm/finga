// Package config is the gateway service's configuration (the WebSocket connections and the REST entry):
// config.yaml beside this file, with only the keys the service uses
// (Low-level design, page 11). Every key can be overridden by FINGA_ and
// its path in upper case.
package config

import (
	"time"

	"finga/internal/host"
)

// Config is services/gateway/config/config.yaml.
type Config struct {
	HTTP          HTTP          `mapstructure:"http"`
	ShutdownGrace time.Duration `mapstructure:"shutdown_grace" validate:"gt=0"`
	LogLevel      string        `mapstructure:"log_level" validate:"oneof=debug info warn error"`
	Dependencies  Dependencies  `mapstructure:"dependencies"`
}

// HTTP is where the gateway serves clients: REST under /api, the WebSocket
// at /ws, and /healthz, /readyz and /metrics.
type HTTP struct {
	Addr        string   `mapstructure:"addr" validate:"required"`
	CORSOrigins []string `mapstructure:"cors_origins" validate:"dive,url"` // empty: same origin only
}

// Dependencies are the addresses this service dials the services it calls
// at (Compose sets the services' names).
type Dependencies struct {
	Account   string `mapstructure:"account" validate:"required"`
	Character string `mapstructure:"character" validate:"required"`
	Item      string `mapstructure:"item" validate:"required"`
	World     string `mapstructure:"world" validate:"required"`
	Social    string `mapstructure:"social" validate:"required"`
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{ShutdownGrace: c.ShutdownGrace, LogLevel: c.LogLevel}
}
