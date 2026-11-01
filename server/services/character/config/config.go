// Package config is the character service's configuration; any key can be overridden by FINGA_<PATH>.
package config

import (
	"time"

	"finga/internal/host"
	"finga/pkg/dbx"
	"finga/pkg/logger"
)

// Config is services/character/config/config.yaml.
type Config struct {
	GRPC          host.GRPCSection `mapstructure:"grpc"`
	Ops           host.OpsSection  `mapstructure:"ops"`
	ShutdownGrace time.Duration    `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log           logger.Config    `mapstructure:"log"`
	DataDir       string           `mapstructure:"data_dir" validate:"required"`
	Database      dbx.Config       `mapstructure:"database"`
	Dependencies  Dependencies     `mapstructure:"dependencies"`
}

// Dependencies are the addresses of the services this one calls.
type Dependencies struct {
	Item string `mapstructure:"item" validate:"required"`
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{GRPC: c.GRPC.Listen, Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, Log: c.Log}
}
