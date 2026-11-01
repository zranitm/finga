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

	"finga/pkg/configload"
	"finga/pkg/observability"
)

const buildTimeout = 30 * time.Second

// Process is a built binary: Run until ctx ends, then Close.
type Process interface {
	Run(ctx context.Context) error
	Close()
}

// Build is a binary's composition root.
type Build[C Config] func(ctx context.Context, cfg C, log *slog.Logger, reg *prometheus.Registry) (Process, error)

// Main runs Start until SIGINT or SIGTERM and returns the exit code.
func Main[C Config](name, version string, build Build[C]) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return Start(ctx, os.Stdout, os.Stderr, name, version, build)
}

// Start loads the configuration, builds the process and runs it until ctx ends; config errors go to stderr.
func Start[C Config](ctx context.Context, stdout, stderr io.Writer, name, version string, build Build[C]) int {
	cfg, err := configload.Load[C](configload.Options{Path: configload.ResolvePath(ConfigDir(name)), EnvPrefix: EnvPrefix, Strict: true})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, name+": invalid configuration:", err)
		return 1
	}
	level, err := observability.ParseLevel((*cfg).Host().LogLevel)
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
