// Package config is the item service's configuration (items, the inventory and item events):
// config.yaml beside this file, with only the keys the service uses
// (Low-level design, page 11). Every key can be overridden by FINGA_ and
// its path in upper case.
package config

import (
	"time"

	"finga/internal/host"
	"finga/pkg/dbx"
)

// Config is services/item/config/config.yaml.
type Config struct {
	GRPC          host.GRPCSection `mapstructure:"grpc"`
	Ops           host.OpsSection  `mapstructure:"ops"`
	ShutdownGrace time.Duration    `mapstructure:"shutdown_grace" validate:"gt=0"`
	LogLevel      string           `mapstructure:"log_level" validate:"oneof=debug info warn error"`
	DataDir       string           `mapstructure:"data_dir" validate:"required"`
	Database      dbx.Config       `mapstructure:"database"`
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{GRPC: c.GRPC.Listen, Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, LogLevel: c.LogLevel}
}
