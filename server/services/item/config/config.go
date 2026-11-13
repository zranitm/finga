// Package config is the item module's section of config/config.yaml; FINGA_ITEM_<PATH> overrides a key.
package config

import (
	"fmt"
	"time"

	"finga/internal/host"
	"finga/pkg/idempotency"
)

// Config is the item section; grpc and ops run the module as its own process, until one binary runs every module.
type Config struct {
	GRPC            host.GRPCSection `mapstructure:"grpc"`
	Ops             host.OpsSection  `mapstructure:"ops"`
	IdempotencyKeys IdempotencyKeys  `mapstructure:"idempotency_keys"`
	Outbox          Outbox           `mapstructure:"outbox"`
}

// IdempotencyKeys is how long a command's key replays its first answer, and how older keys are deleted.
type IdempotencyKeys struct {
	Keep       time.Duration `mapstructure:"keep" validate:"gt=0"`         // never under idempotency.MinKeep, as a retry after it runs the command again
	PruneEvery time.Duration `mapstructure:"prune_every" validate:"gt=0"`  // how often older keys are deleted
	PruneBatch int           `mapstructure:"prune_batch" validate:"min=1"` // keys per DELETE
	PruneGap   time.Duration `mapstructure:"prune_gap" validate:"gte=0"`   // the pause between DELETEs
}

// Outbox is how long outbox rows are kept, and how older ones are deleted.
type Outbox struct {
	PublishedKeep   time.Duration `mapstructure:"published_keep" validate:"gt=0"`    // a published row goes this long after published_at
	UnpublishedKeep time.Duration `mapstructure:"unpublished_keep" validate:"gte=0"` // Stage 1 only, 0 is off: Stage 2's relay needs it off, or its unsent events are deleted
	PruneEvery      time.Duration `mapstructure:"prune_every" validate:"gt=0"`       // how often older rows are deleted
	PruneBatch      int           `mapstructure:"prune_batch" validate:"min=1"`      // rows per DELETE
	PruneGap        time.Duration `mapstructure:"prune_gap" validate:"gte=0"`        // the pause between DELETEs
}

// Validate refuses a key kept less than the idempotency library's floor.
func (c Config) Validate() error {
	if c.IdempotencyKeys.Keep < idempotency.MinKeep {
		return fmt.Errorf("idempotency_keys.keep: %s is under %s, so a retry could run its command twice", c.IdempotencyKeys.Keep, idempotency.MinKeep)
	}
	return nil
}
