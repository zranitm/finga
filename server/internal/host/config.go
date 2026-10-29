package host

import (
	"path/filepath"
	"time"
)

// EnvPrefix starts every override: a key's path in upper case after it
// (database.url is FINGA_DATABASE_URL), read by the one binary that has
// the key.
const EnvPrefix = "FINGA"

// GRPCSection is the section where a service serves its gRPC API.
type GRPCSection struct {
	Listen string `mapstructure:"listen" validate:"required"`
}

// OpsSection is the section where a service serves /healthz, /readyz and /metrics.
type OpsSection struct {
	Addr string `mapstructure:"addr" validate:"required"`
}

// Settings is what the host reads of a service's configuration: the
// sections every service's file has (Low-level design, page 11).
type Settings struct {
	GRPC          string // the gRPC listener; empty for the gateway, which serves HTTP
	Ops           string // the ops listener; empty for the gateway, whose HTTP listener serves them
	ShutdownGrace time.Duration
	LogLevel      string
}

// Config is a service's own configuration type, which Start loads from the
// service's own file.
type Config interface {
	Host() Settings
}

// ConfigDir is where a service's configuration file lives, relative to
// server/, where the binaries run (and /app in an image).
func ConfigDir(service string) string {
	return filepath.Join("services", service, "config")
}
