package dbx

import "time"

// Config is a pool's settings from a config file's database section; a zero timeout keeps the server's default, none.
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
