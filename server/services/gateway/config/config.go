// Package config is the gateway module's section of config/config.yaml; FINGA_GATEWAY_<PATH> overrides a key.
package config

import (
	"time"

	"finga/internal/host"
)

// Config is the gateway section; ops and dependencies run the module as its own process, until one binary runs every module.
type Config struct {
	Ops          host.OpsSection `mapstructure:"ops"`
	WS           WS              `mapstructure:"ws"`
	Sessions     Sessions        `mapstructure:"sessions"`
	Dependencies Dependencies    `mapstructure:"dependencies"`
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
