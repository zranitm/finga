// Package config is the world module's section of config/config.yaml; FINGA_WORLD_<PATH> overrides a key.
package config

import (
	"time"

	"finga/internal/host"
)

// Config is the world section; grpc, ops and dependencies run the module as its own process, until one binary runs every module.
type Config struct {
	GRPC               host.GRPCSection `mapstructure:"grpc"`
	Ops                host.OpsSection  `mapstructure:"ops"`
	TickRateHz         int              `mapstructure:"tick_rate_hz" validate:"min=1"`
	CheckpointInterval time.Duration    `mapstructure:"checkpoint_interval" validate:"gt=0"`
	FlushTimeout       time.Duration    `mapstructure:"flush_timeout" validate:"gt=0"`
	ItemCalls          ItemCalls        `mapstructure:"item_calls"`
	Dependencies       Dependencies     `mapstructure:"dependencies"`
}

// ItemCalls is the worker pool that runs the calls to the item service off the tick, and how many drops one grant call may carry: at most the item service's 32.
type ItemCalls struct {
	Workers  int `mapstructure:"workers" validate:"min=1"`
	BatchMax int `mapstructure:"batch_max" validate:"min=1,max=32"`
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
