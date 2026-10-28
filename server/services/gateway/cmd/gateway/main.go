// Command gateway runs the gateway service, the only public entry
// (Low-level design, page 10): REST under /api and the WebSocket gateway at
// /ws, with no game rules and no database. It calls every other service
// over gRPC, follows their delivery streams, and runs until SIGINT or
// SIGTERM, draining its connections first.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/config"
	"finga/internal/host"
	"finga/services/gateway/cmd/gateway/app"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	os.Exit(host.Main("gateway", version, func(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (host.Process, error) {
		return app.Build(ctx, app.BuildOptions{Config: cfg, Logger: log, Registry: reg})
	}))
}
