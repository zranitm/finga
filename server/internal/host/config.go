package host

import (
	"time"

	"finga/pkg/dbx"
	"finga/pkg/observability/logger"
	"finga/pkg/observability/tracing"
)

// EnvPrefix starts every environment override: database.url is FINGA_DATABASE_URL, world.tick_rate_hz FINGA_WORLD_TICK_RATE_HZ.
const EnvPrefix = "FINGA"

// ConfigDir holds the server's configuration, relative to server/ (/app in an image).
const ConfigDir = "config"

// HTTPSection is where the process serves players: REST and the WebSocket.
type HTTPSection struct {
	Addr        string   `mapstructure:"addr" validate:"required"`
	CORSOrigins []string `mapstructure:"cors_origins" validate:"dive,url"`
}

// GRPCSection is where a module's own process serves its gRPC API.
type GRPCSection struct {
	Listen string `mapstructure:"listen" validate:"required"`
}

// OpsSection is where a process serves /healthz, /readyz and /metrics.
type OpsSection struct {
	Addr string `mapstructure:"addr" validate:"required"`
}

// Settings are what the host runs a process with.
type Settings struct {
	GRPC          string // empty for a process that serves no gRPC
	Ops           string
	ShutdownGrace time.Duration
	Log           logger.Config
	Tracing       tracing.Config
	DataDir       string     // the game data, which GameData loads
	Database      dbx.Config // the pool Module.Database opens
}

// Config is the server's configuration type, loaded by Start.
type Config interface {
	// Host is the settings of the process named process.
	Host(process string) (Settings, error)
}
