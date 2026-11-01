// Package config is the gateway's config.yaml; any key can be overridden by FINGA_ plus its path in upper case.
package config

import (
	"time"

	"finga/internal/host"
	"finga/pkg/logger"
)

// Config is services/gateway/config/config.yaml.
type Config struct {
	HTTP          HTTP          `mapstructure:"http"`
	ShutdownGrace time.Duration `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log           logger.Config `mapstructure:"log"`
	Dependencies  Dependencies  `mapstructure:"dependencies"`
}

// HTTP is where the gateway serves clients.
type HTTP struct {
	Addr        string   `mapstructure:"addr" validate:"required"`
	CORSOrigins []string `mapstructure:"cors_origins" validate:"dive,url"` // empty: same origin only
}

// Dependencies are the addresses of the services the gateway dials.
type Dependencies struct {
	Account   string `mapstructure:"account" validate:"required"`
	Character string `mapstructure:"character" validate:"required"`
	Item      string `mapstructure:"item" validate:"required"`
	World     string `mapstructure:"world" validate:"required"`
	Social    string `mapstructure:"social" validate:"required"`
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{ShutdownGrace: c.ShutdownGrace, Log: c.Log}
}
