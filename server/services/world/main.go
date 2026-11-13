// Command world runs the world service.
package main

import (
	"context"
	"os"

	"finga/config"
	"finga/internal/host"
	"finga/services/world/app"
)

func main() {
	os.Exit(host.Main("world", func(ctx context.Context, h *host.Host, cfg config.Config) error {
		return app.Build(ctx, h, cfg.World)
	}))
}
