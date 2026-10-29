// Command social runs the social service: parties, invites and chat, in
// memory, served over gRPC, delivering party traffic to each gateway and
// party membership to each world node (Low-level design, page 10). It runs
// until SIGINT or SIGTERM.
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

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	os.Exit(host.Main("social", version, func(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (host.Process, error) {
		return app.Build(ctx, cfg, log, reg)
	}))
}
