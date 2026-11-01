// Package config loads the item service's config.yaml; FINGA_<KEY_PATH> overrides any key.
package config

import (
	"time"

	"finga/internal/host"
	"finga/pkg/dbx"
	"finga/pkg/logger"
)

// Config is services/item/config/config.yaml.
type Config struct {
	GRPC          host.GRPCSection `mapstructure:"grpc"`
	Ops           host.OpsSection  `mapstructure:"ops"`
	ShutdownGrace time.Duration    `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log           logger.Config    `mapstructure:"log"`
	DataDir       string           `mapstructure:"data_dir" validate:"required"`
	Database      dbx.Config       `mapstructure:"database"`
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{GRPC: c.GRPC.Listen, Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, Log: c.Log}
}
