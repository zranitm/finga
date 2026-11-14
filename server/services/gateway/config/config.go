// Package config is the gateway module's section of config/config.yaml; FINGA_GATEWAY_<PATH> overrides a key.
package config

import "time"

// Config is the gateway section.
type Config struct {
	WS       WS       `mapstructure:"ws"`
	Sessions Sessions `mapstructure:"sessions"`
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
