// Package config loads the item service's config.yaml; FINGA_<KEY_PATH> overrides any key.
package config

import (
	"fmt"
	"time"

	"finga/internal/host"
	"finga/pkg/dbx"
	"finga/pkg/idempotency"
	"finga/pkg/logger"
	"finga/pkg/tracing"
)

// Config is services/item/config/config.yaml.
type Config struct {
	GRPC            host.GRPCSection `mapstructure:"grpc"`
	Ops             host.OpsSection  `mapstructure:"ops"`
	ShutdownGrace   time.Duration    `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log             logger.Config    `mapstructure:"log"`
	Tracing         tracing.Config   `mapstructure:"tracing"`
	DataDir         string           `mapstructure:"data_dir" validate:"required"`
	Database        dbx.Config       `mapstructure:"database"`
	IdempotencyKeys IdempotencyKeys  `mapstructure:"idempotency_keys"`
}

// IdempotencyKeys is how long a command's key replays its first answer, and how older keys are deleted.
type IdempotencyKeys struct {
	Keep       time.Duration `mapstructure:"keep" validate:"gt=0"`         // never under idempotency.MinKeep, as a retry after it runs the command again
	PruneEvery time.Duration `mapstructure:"prune_every" validate:"gt=0"`  // how often older keys are deleted
	PruneBatch int           `mapstructure:"prune_batch" validate:"min=1"` // keys per DELETE
	PruneGap   time.Duration `mapstructure:"prune_gap" validate:"gte=0"`   // the pause between DELETEs
}

// Validate refuses a key kept less than the idempotency library's floor.
func (c Config) Validate() error {
	if c.IdempotencyKeys.Keep < idempotency.MinKeep {
		return fmt.Errorf("idempotency_keys.keep: %s is under %s, so a retry could run its command twice", c.IdempotencyKeys.Keep, idempotency.MinKeep)
	}
	return nil
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{GRPC: c.GRPC.Listen, Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, Log: c.Log, Tracing: c.Tracing}
}
