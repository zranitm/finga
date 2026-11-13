// Command social serves parties, invites and chat over gRPC, in memory, until SIGINT or SIGTERM.
package main

import (
	"context"
	"os"

	"finga/config"
	"finga/internal/host"
	"finga/services/social/app"
)

func main() {
	os.Exit(host.Main("social", func(ctx context.Context, h *host.Host, cfg config.Config) error {
		return app.Build(ctx, h, cfg.Social)
	}))
}
