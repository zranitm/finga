package host

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/config"
	"finga/pkg/configload"
	"finga/pkg/observability"
)

const (
	configDir    = "configs"
	buildTimeout = 30 * time.Second
)

// Process is a built binary: it runs until its context ends, then Close
// releases what the build opened.
type Process interface {
	Run(ctx context.Context) error
	Close()
}

// Build is a binary's composition root: it wires the process from the
// configuration, with a logger and a metrics registry.
type Build func(ctx context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (Process, error)

// Main is a service binary's main: it runs Start until SIGINT or SIGTERM
// and returns the exit code.
func Main(name, version string, build Build) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Start(ctx, os.Stdout, os.Stderr, name, version, build)
}

// Start loads the configuration (the YAML file and the environment, nothing
// else; whoever starts the process sets the environment), builds the
// process, and runs it until ctx ends. Configuration errors go to stderr,
// since there is no logger until the level is known; the loader never
// echoes secrets.
func Start(ctx context.Context, stdout, stderr io.Writer, name, version string, build Build) int {
	cfg, err := configload.Load[config.Config](configload.Options{Path: configload.ResolvePath(configDir), EnvPrefix: config.EnvPrefix, Strict: true})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, name+": invalid configuration:", err)
		return 1
	}
	level, err := observability.ParseLevel(cfg.Telemetry.LogLevel)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, name+":", err)
		return 1
	}
	log := observability.NewLogger(stdout, level)
	buildCtx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	p, err := build(buildCtx, *cfg, log, observability.NewRegistry())
	if err != nil {
		log.ErrorContext(ctx, "startup failed", "service", name, "err", err)
		return 1
	}
	defer p.Close()
	log.InfoContext(ctx, "starting", "service", name, "version", version)
	if err := p.Run(ctx); err != nil {
		log.ErrorContext(ctx, "stopped with an error", "service", name, "err", err)
		return 1
	}
	log.InfoContext(ctx, "stopped", "service", name)
	return 0
}
