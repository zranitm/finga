// Command account serves accounts, sessions and settings over gRPC until SIGINT or SIGTERM.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/host"
	"finga/services/account/app"
	"finga/services/account/config"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	os.Exit(host.Main("account", version, func(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (host.Process, error) {
		return app.Build(ctx, cfg, log, reg)
	}))
}
