// Command character runs the character service.
package main

import (
	"context"
	"os"

	"finga/config"
	"finga/internal/host"
	"finga/services/character/app"
)

func main() {
	os.Exit(host.Main("character", func(ctx context.Context, h *host.Host, cfg config.Config) error {
		return app.Build(ctx, h, cfg.Character)
	}))
}
