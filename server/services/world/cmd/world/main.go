// Command world runs the world service: the map instances, ticking at
// 20 Hz, their mobs, combat, drops and portals, served over gRPC to the
// gateways (Low-level design, pages 3 and 10). It runs until SIGINT or
// SIGTERM.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/host"
	"finga/services/world/cmd/world/app"
	"finga/services/world/config"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	os.Exit(host.Main("world", version, func(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (host.Process, error) {
		return app.Build(ctx, cfg, log, reg)
	}))
}
