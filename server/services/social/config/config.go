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
	Members       Members          `mapstructure:"members"`
}

// Members is how long a member offline and in no party is remembered, and how older ones are forgotten.
type Members struct {
	Keep       time.Duration `mapstructure:"keep" validate:"min=5m"`       // after going offline; never under a few minutes, as a gateway back within its 30 s grace re-registers from it
	PruneEvery time.Duration `mapstructure:"prune_every" validate:"gt=0"`  // how often older ones are forgotten
	PruneBatch int           `mapstructure:"prune_batch" validate:"min=1"` // members per pass of the store's lock
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{GRPC: c.GRPC.Listen, Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, Log: c.Log, Tracing: c.Tracing}
}
