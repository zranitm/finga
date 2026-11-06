// Package config is the world service's configuration; any key can be overridden by FINGA_<PATH>.
package config

import (
	"time"

	"finga/internal/host"
	"finga/pkg/logger"
)

// Config is services/world/config/config.yaml.
type Config struct {
	GRPC               host.GRPCSection `mapstructure:"grpc"`
	Ops                host.OpsSection  `mapstructure:"ops"`
	ShutdownGrace      time.Duration    `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log                logger.Config    `mapstructure:"log"`
	DataDir            string           `mapstructure:"data_dir" validate:"required"`
	TickRateHz         int              `mapstructure:"tick_rate_hz" validate:"min=1"`
	CheckpointInterval time.Duration    `mapstructure:"checkpoint_interval" validate:"gt=0"`
	FlushTimeout       time.Duration    `mapstructure:"flush_timeout" validate:"gt=0"`
	Economy            Economy          `mapstructure:"economy"`
	Dependencies       Dependencies     `mapstructure:"dependencies"`
}

// Economy is the worker pool that runs pickups and inventory actions off the tick.
type Economy struct {
	Workers int `mapstructure:"workers" validate:"min=1"`
}

// TickInterval is the time between two ticks of a map.
func (c Config) TickInterval() time.Duration {
	return time.Second / time.Duration(c.TickRateHz)
}

// Dependencies are the addresses of the services this one calls.
type Dependencies struct {
	Character string `mapstructure:"character" validate:"required"`
	Item      string `mapstructure:"item" validate:"required"`
	Social    string `mapstructure:"social" validate:"required"`
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{GRPC: c.GRPC.Listen, Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, Log: c.Log}
}
