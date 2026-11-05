// Package config is the gateway's config.yaml; any key can be overridden by FINGA_ plus its path in upper case.
package config

import (
	"time"

	"finga/internal/host"
	"finga/pkg/logger"
)

// Config is services/gateway/config/config.yaml.
type Config struct {
	HTTP          HTTP            `mapstructure:"http"`
	Ops           host.OpsSection `mapstructure:"ops"`
	WS            WS              `mapstructure:"ws"`
	Sessions      Sessions        `mapstructure:"sessions"`
	ShutdownGrace time.Duration   `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log           logger.Config   `mapstructure:"log"`
	Dependencies  Dependencies    `mapstructure:"dependencies"`
}

// HTTP is where the gateway serves players.
type HTTP struct {
	Addr        string   `mapstructure:"addr" validate:"required"`
	CORSOrigins []string `mapstructure:"cors_origins" validate:"dive,url"` // empty: same origin only
}

// WS is how each WebSocket connection runs.
type WS struct {
	HelloTimeout time.Duration `mapstructure:"hello_timeout" validate:"gt=0"`
	CommandQueue int           `mapstructure:"command_queue" validate:"min=1"`
}

// Sessions is how the gateway keeps its players' sessions.
type Sessions struct {
	JoinTimeout   time.Duration `mapstructure:"join_timeout" validate:"gt=0"`
	CheckInterval time.Duration `mapstructure:"check_interval" validate:"gt=0"`
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
	return host.Settings{Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, Log: c.Log}
}
