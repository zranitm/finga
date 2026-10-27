// Command gameserver runs what is left of the game in one process while
// the services leave it (Low-level design, page 10): REST under /api, the
// WebSocket gateway at /ws, the map instances, and the background workers.
// It calls the extracted services over gRPC, and runs until SIGINT or
// SIGTERM.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"finga/cmd/gameserver/app"
	"finga/internal/config"
	"finga/internal/host"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	os.Exit(host.Main("gameserver", version, func(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (host.Process, error) {
		return app.Build(ctx, app.BuildOptions{Config: cfg, Logger: log, Registry: reg})
	}))
}
