package config

import "time"

// WS is how each WebSocket connection runs; FINGA_WS_<PATH> overrides a key.
type WS struct {
	HelloTimeout time.Duration `mapstructure:"hello_timeout" validate:"gt=0"`
	CommandQueue int           `mapstructure:"command_queue" validate:"min=1"`
}

// HTTP is where the server serves players, and how hard each account may call the REST API there; FINGA_HTTP_<PATH> overrides a key.
type HTTP struct {
	Addr         string       `mapstructure:"addr" validate:"required"`
	CORSOrigins  []string     `mapstructure:"cors_origins" validate:"dive,url"`
	AccountLimit AccountLimit `mapstructure:"account_limit"`
}

// AccountLimit is how fast each account may call the REST API once logged in: a token bucket refilling PerSecond, up to Burst.
type AccountLimit struct {
	PerSecond float64 `mapstructure:"per_second" validate:"gt=0"`
	Burst     int     `mapstructure:"burst" validate:"min=1"`
}
