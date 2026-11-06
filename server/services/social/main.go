// Command social serves parties, invites and chat over gRPC, in memory, until SIGINT or SIGTERM.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/host"
	"finga/services/social/app"
	"finga/services/social/config"
)

func main() {
	os.Exit(host.Main("social", func(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (host.Process, error) {
		return app.Build(ctx, cfg, log, reg)
	}))
}
