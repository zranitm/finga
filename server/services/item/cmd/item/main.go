// Command item runs the item service: item instances, the inventory, item
// events, and the idempotency keys and outbox of its transactions, served
// over gRPC (Low-level design, page 10). It runs until SIGINT or SIGTERM.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/host"
	"finga/services/item/cmd/item/app"
	"finga/services/item/config"
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() {
	os.Exit(host.Main("item", version, func(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (host.Process, error) {
		return app.Build(ctx, cfg, log, reg)
	}))
}
