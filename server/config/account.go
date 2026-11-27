package config

import "time"

// Account is the account module's section of config/config.yaml; FINGA_ACCOUNT_<PATH> overrides a key.
type Account struct {
	Password    Password           `mapstructure:"password"`
	Sessions    AccountSessions    `mapstructure:"sessions"`
	Connections AccountConnections `mapstructure:"connections"`
}

// AccountConnections is how the account module keeps its players' live connections.
type AccountConnections struct {
	JoinTimeout   time.Duration `mapstructure:"join_timeout" validate:"gt=0"`   // a Hello's or a rejoin's whole join, retries included
	CheckInterval time.Duration `mapstructure:"check_interval" validate:"gt=0"` // how often every session is checked with the account module
}

// AccountSessions is how long a session is kept once it ended, revoked or expired, and how older ones are deleted.
type AccountSessions struct {
	Keep       time.Duration `mapstructure:"keep" validate:"gt=0"`         // after the session ended
	PruneEvery time.Duration `mapstructure:"prune_every" validate:"gt=0"`  // how often older ones are deleted
	PruneBatch int           `mapstructure:"prune_batch" validate:"min=1"` // sessions per DELETE
	PruneGap   time.Duration `mapstructure:"prune_gap" validate:"gte=0"`   // the pause between DELETEs
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
