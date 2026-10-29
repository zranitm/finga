// Command account runs the account service: accounts, login sessions and
// their tokens, and settings, served over gRPC, with WatchSessions telling
// every gateway which sessions ended (Low-level design, page 10). It runs
// until SIGINT or SIGTERM.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/host"
	"finga/services/account/cmd/account/app"
	"finga/services/account/config"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	os.Exit(host.Main("account", version, func(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (host.Process, error) {
		return app.Build(ctx, cfg, log, reg)
	}))
}
