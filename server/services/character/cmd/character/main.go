// Command character runs the character service: characters, their stats,
// levels and EXP, and the checkpoints the world saves, served over gRPC
// (Low-level design, page 10). It runs until SIGINT or SIGTERM.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/host"
	"finga/services/character/cmd/character/app"
	"finga/services/character/config"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	os.Exit(host.Main("character", version, func(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (host.Process, error) {
		return app.Build(ctx, cfg, log, reg)
	}))
}
