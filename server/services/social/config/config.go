// Package config is the social module's section of config/config.yaml; FINGA_SOCIAL_<PATH> overrides a key.
package config

import (
	"time"

	"finga/internal/host"
)

// Config is the social section; grpc and ops run the module as its own process, until one binary runs every module.
type Config struct {
	GRPC    host.GRPCSection `mapstructure:"grpc"`
	Ops     host.OpsSection  `mapstructure:"ops"`
	Members Members          `mapstructure:"members"`
}

// Members is how long a member offline and in no party is remembered, and how older ones are forgotten.
type Members struct {
	Keep       time.Duration `mapstructure:"keep" validate:"min=5m"`       // after going offline; never under a few minutes, as a gateway back within its 30 s grace re-registers from it
	PruneEvery time.Duration `mapstructure:"prune_every" validate:"gt=0"`  // how often older ones are forgotten
	PruneBatch int           `mapstructure:"prune_batch" validate:"min=1"` // members per pass of the store's lock
	PruneGap   time.Duration `mapstructure:"prune_gap" validate:"gte=0"`   // the pause between passes
}
