package dbx

import "time"

// Config is a pool's settings from a configuration file's database section. A zero timeout leaves the
// server's default, which is none.
type Config struct {
	URL      string `mapstructure:"url" validate:"required,url,startswith=postgres"`
	MaxConns int32  `mapstructure:"max_conns" validate:"min=1"`
	// StatementTimeout ends a statement that runs longer.
	StatementTimeout time.Duration `mapstructure:"statement_timeout" validate:"gte=0"`
	// LockTimeout ends a statement that waits longer for a lock.
	LockTimeout time.Duration `mapstructure:"lock_timeout" validate:"gte=0"`
	// IdleInTransactionTimeout ends a session that sits in a transaction longer without a statement.
	IdleInTransactionTimeout time.Duration `mapstructure:"idle_in_transaction_timeout" validate:"gte=0"`
}
