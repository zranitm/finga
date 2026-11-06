// Package config loads the social service's config.yaml; FINGA_<KEY_PATH> overrides any key.
package config

import (
	"time"

	"finga/internal/host"
	"finga/pkg/logger"
	"finga/pkg/tracing"
)

// Config is services/social/config/config.yaml.
type Config struct {
	GRPC          host.GRPCSection `mapstructure:"grpc"`
	Ops           host.OpsSection  `mapstructure:"ops"`
	ShutdownGrace time.Duration    `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log           logger.Config    `mapstructure:"log"`
	Tracing       tracing.Config   `mapstructure:"tracing"`
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{GRPC: c.GRPC.Listen, Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, Log: c.Log, Tracing: c.Tracing}
}
