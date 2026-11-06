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
	GRPC            host.GRPCSection `mapstructure:"grpc"`
	Ops             host.OpsSection  `mapstructure:"ops"`
	ShutdownGrace   time.Duration    `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log             logger.Config    `mapstructure:"log"`
	DataDir         string           `mapstructure:"data_dir" validate:"required"`
	Database        dbx.Config       `mapstructure:"database"`
	IdempotencyKeys IdempotencyKeys  `mapstructure:"idempotency_keys"`
}

// IdempotencyKeys is how long a command's key replays its first answer, and how older keys are deleted.
type IdempotencyKeys struct {
	Keep       time.Duration `mapstructure:"keep" validate:"gt=0"`         // a retry after it runs the command again
	PruneEvery time.Duration `mapstructure:"prune_every" validate:"gt=0"`  // how often older keys are deleted
	PruneBatch int           `mapstructure:"prune_batch" validate:"min=1"` // keys per DELETE
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{GRPC: c.GRPC.Listen, Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, Log: c.Log}
}
