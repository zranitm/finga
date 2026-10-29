// Package config is the world service's configuration (the map instances):
// config.yaml beside this file, with only the keys the service uses
// (Low-level design, page 11). Every key can be overridden by FINGA_ and
// its path in upper case.
package config

import (
	"errors"
	"time"

	"finga/internal/host"
)

// Config is services/world/config/config.yaml.
type Config struct {
	GRPC               host.GRPCSection `mapstructure:"grpc"`
	Ops                host.OpsSection  `mapstructure:"ops"`
	ShutdownGrace      time.Duration    `mapstructure:"shutdown_grace" validate:"gt=0"`
	LogLevel           string           `mapstructure:"log_level" validate:"oneof=debug info warn error"`
	DataDir            string           `mapstructure:"data_dir" validate:"required"`
	TickRateHz         int              `mapstructure:"tick_rate_hz" validate:"min=1,max=60"`
	CheckpointInterval time.Duration    `mapstructure:"checkpoint_interval" validate:"gt=0"`
	Economy            Economy          `mapstructure:"economy"`
	Dependencies       Dependencies     `mapstructure:"dependencies"`
}

// Economy is the worker pool that runs pickups and inventory actions off
// the tick (page 3).
type Economy struct {
	Workers int `mapstructure:"workers" validate:"min=1"`
}

// subStepsPerSecond is the movement simulation's fixed step (1/60 s).
const subStepsPerSecond = 60

// TickInterval is the time between two ticks of a map.
func (c Config) TickInterval() time.Duration {
	return time.Second / time.Duration(c.TickRateHz)
}

// Validate is the rule no tag can express: each tick holds whole sub-steps.
func (c *Config) Validate() error {
	if subStepsPerSecond%c.TickRateHz != 0 {
		return errors.New("tick_rate_hz must divide 60, so each tick holds whole 1/60 s sub-steps")
	}
	return nil
}

// Dependencies are the addresses this service dials the services it calls
// at (Compose sets the services' names).
type Dependencies struct {
	Character string `mapstructure:"character" validate:"required"`
	Item      string `mapstructure:"item" validate:"required"`
	Social    string `mapstructure:"social" validate:"required"`
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{GRPC: c.GRPC.Listen, Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, LogLevel: c.LogLevel}
}
