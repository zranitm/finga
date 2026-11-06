// Package config loads the account service's config.yaml; FINGA_<KEY_PATH> overrides any key.
package config

import (
	"time"

	"finga/internal/host"
	"finga/pkg/dbx"
	"finga/pkg/logger"
)

// Config is services/account/config/config.yaml.
type Config struct {
	GRPC          host.GRPCSection `mapstructure:"grpc"`
	Ops           host.OpsSection  `mapstructure:"ops"`
	ShutdownGrace time.Duration    `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log           logger.Config    `mapstructure:"log"`
	Database      dbx.Config       `mapstructure:"database"`
	Password      Password         `mapstructure:"password"`
}

// Password is the Argon2id cost of a password hash, raised as the hardware allows; a login rehashes an older one.
type Password struct {
	Time      uint32 `mapstructure:"time" validate:"min=1"`          // passes over the memory
	MemoryKiB uint32 `mapstructure:"memory_kib" validate:"min=8192"` // the memory each hash fills
	Threads   uint8  `mapstructure:"threads" validate:"min=1"`       // lanes hashed in parallel
	// Concurrent hashes at most, each holding MemoryKiB; past them a login waits up to Wait, then is refused.
	Concurrent int64         `mapstructure:"concurrent" validate:"min=1"`
	Wait       time.Duration `mapstructure:"wait" validate:"gt=0"`
}

// Host is the part of the configuration the host runs the service with.
func (c Config) Host() host.Settings {
	return host.Settings{GRPC: c.GRPC.Listen, Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, Log: c.Log}
}
