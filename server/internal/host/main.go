package host

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/pkg/configload"
	"finga/pkg/logger"
	"finga/pkg/observability"
)

const (
	buildTimeout       = 30 * time.Second
	healthcheckTimeout = 3 * time.Second
)

// Process is a built binary: Run until ctx ends, then Close.
type Process interface {
	Run(ctx context.Context) error
	Close()
}

// Build is a binary's composition root.
type Build[C Config] func(ctx context.Context, cfg C, log *slog.Logger, reg *prometheus.Registry) (Process, error)

// Main runs Start until SIGINT or SIGTERM and returns the exit code; a second signal kills the process.
// With healthcheckArg it probes the running service instead.
func Main[C Config](name, version string, build Build[C]) int {
	if slices.Equal(os.Args[1:], []string{healthcheckArg}) {
		ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
		defer cancel()
		return Healthcheck[C](ctx, os.Stderr, name)
	}
	signalled, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The shutdown starts only once the handler is gone, so the next signal gets the default action.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	context.AfterFunc(signalled, func() { stop(); cancel() })
	return Start(ctx, os.Stdout, os.Stderr, name, version, build)
}

// Start loads the configuration, builds the process and runs it until ctx ends; config errors go to stderr.
func Start[C Config](ctx context.Context, stdout, stderr io.Writer, name, version string, build Build[C]) int {
	cfg, err := loadConfig[C](name)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, name+": invalid configuration:", err)
		return 1
	}
	log, err := logger.New(stdout, (*cfg).Host().Log)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, name+":", err)
		return 1
	}
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

func loadConfig[C Config](name string) (*C, error) {
	return configload.Load[C](configload.Options{Path: configload.ResolvePath(ConfigDir(name)), EnvPrefix: EnvPrefix, Strict: true})
}
