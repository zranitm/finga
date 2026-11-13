// Command gateway runs the gateway service, the only public entry: REST under /api and the WebSocket at /ws.
package main

import (
	"context"
	"os"

	"finga/config"
	"finga/internal/host"
	"finga/services/gateway/app"
)

func main() {
	os.Exit(host.Main("gateway", func(ctx context.Context, h *host.Host, cfg config.Config) error {
		return app.Build(ctx, h, cfg.Gateway, cfg.HTTP)
	}))
}
