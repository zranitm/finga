// Command gameserver runs the game: REST under /api, the WebSocket gateway
// at /ws, the map instances, and the background workers. It loads
// configuration, builds the server, and runs until SIGINT or SIGTERM.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"finga/internal/config"
	"finga/internal/server"
	"finga/pkg/configload"
	"finga/pkg/observability"
)

const (
	configDir    = "configs"
	dotEnvFile   = ".env"
	buildTimeout = 30 * time.Second
)

// version is set at build time (-ldflags "-X main.version=...").
var version = "dev"

func main() { os.Exit(run()) }

func run() int {
	// Configuration first: there is no logger until the level is known, so
	// configuration errors go to stderr. The loader never echoes secrets.
	if err := configload.LoadDotEnv(dotEnvFile); err != nil {
		fmt.Fprintln(os.Stderr, "gameserver:", err)
		return 1
	}
	cfg, err := configload.Load[config.Config](configload.Options{Path: configload.ResolvePath(configDir), EnvPrefix: config.EnvPrefix, Strict: true})
	if err != nil {
		fmt.Fprintln(os.Stderr, "gameserver: invalid configuration:", err)
		return 1
	}
	level, err := observability.ParseLevel(cfg.Telemetry.LogLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gameserver:", err)
		return 1
	}
	log := observability.NewLogger(os.Stdout, level)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	buildCtx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	srv, err := server.Build(buildCtx, server.BuildOptions{Config: *cfg, Logger: log, Registry: observability.NewRegistry()})
	if err != nil {
		log.Error("startup failed", "err", err)
		return 1
	}
	defer srv.Close()

	log.Info("starting", "version", version, "addr", cfg.Server.Addr)
	if err := srv.Run(ctx); err != nil {
		log.Error("server stopped with an error", "err", err)
		return 1
	}
	log.Info("stopped")
	return 0
}
