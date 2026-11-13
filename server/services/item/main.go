// Command item serves items, inventories and item events over gRPC until SIGINT or SIGTERM.
package main

import (
	"context"
	"os"

	"finga/config"
	"finga/internal/host"
	"finga/services/item/app"
)

func main() {
	os.Exit(host.Main("item", func(ctx context.Context, h *host.Host, cfg config.Config) error {
		return app.Build(ctx, h, cfg.Item)
	}))
}
