// Package config is the character service's configuration (characters, their stats and checkpoints):
// config.yaml beside this file, with only the keys the service uses
// (Low-level design, page 11). Every key can be overridden by FINGA_ and
// its path in upper case.
package config

import (
	"time"

	"finga/internal/host"
	"finga/pkg/dbx"
)

// Config is services/character/config/config.yaml.
type Config struct {
	GRPC          host.GRPCSection `mapstructure:"grpc"`
	Ops           host.OpsSection  `mapstructure:"ops"`
	ShutdownGrace time.Duration    `mapstructure:"shutdown_grace" validate:"gt=0"`
	LogLevel      string           `mapstructure:"log_level" validate:"oneof=debug info warn error"`
	DataDir       string           `mapstructure:"data_dir" validate:"required"`
	Database      dbx.Config       `mapstructure:"database"`
	Dependencies  Dependencies     `mapstructure:"dependencies"`
}

// Dependencies are the addresses this service dials the services it calls
// at (Compose sets the services' names).
type Dependencies struct {
	Item string `mapstructure:"item" validate:"required"`
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{GRPC: c.GRPC.Listen, Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, LogLevel: c.LogLevel}
}
