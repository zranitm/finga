package host

import (
	"path/filepath"
	"time"

	"finga/pkg/logger"
)

// EnvPrefix starts every environment override: database.url is FINGA_DATABASE_URL.
const EnvPrefix = "FINGA"

// GRPCSection is the section where a service serves its gRPC API.
type GRPCSection struct {
	Listen string `mapstructure:"listen" validate:"required"`
}

// OpsSection is the section where a service serves /healthz, /readyz and /metrics.
type OpsSection struct {
	Addr string `mapstructure:"addr" validate:"required"`
}

// Settings holds the sections every service's configuration has.
type Settings struct {
	GRPC          string // empty for the gateway, which serves HTTP
	Ops           string // empty for the gateway, whose HTTP listener serves them
	ShutdownGrace time.Duration
	Log           logger.Config
}

// Config is a service's own configuration type, loaded by Start.
type Config interface {
	Host() Settings
}

// ConfigDir is a service's configuration directory, relative to server/ (/app in an image).
func ConfigDir(service string) string {
	return filepath.Join("services", service, "config")
}
