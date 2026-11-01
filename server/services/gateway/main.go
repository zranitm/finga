// Command gateway runs the gateway service, the only public entry: REST under /api and the WebSocket at /ws.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/host"
	"finga/services/gateway/app"
	"finga/services/gateway/config"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	os.Exit(host.Main("gateway", version, func(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (host.Process, error) {
		return app.Build(ctx, app.BuildOptions{Config: cfg, Logger: log, Registry: reg})
	}))
}
