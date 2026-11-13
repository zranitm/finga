// Command account serves accounts, sessions and settings over gRPC until SIGINT or SIGTERM.
package main

import (
	"context"
	"os"

	"finga/config"
	"finga/internal/host"
	"finga/services/account/app"
)

func main() {
	os.Exit(host.Main("account", func(ctx context.Context, h *host.Host, cfg config.Config) error {
		return app.Build(ctx, h, cfg.Account)
	}))
}
