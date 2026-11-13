package host

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"slices"
	"syscall"
	"time"

	"finga/pkg/configload"
	"finga/pkg/observability/logger"
	"finga/pkg/observability/metrics"
)

const (
	buildTimeout       = 30 * time.Second
	healthcheckTimeout = 3 * time.Second
)

// version is the build's, set at link time: -ldflags "-X finga/internal/host.version=...".
var version = "dev"

// buildVersion is the version services report: the linked one, else the commit a plain go build recorded.
var buildVersion = func() string {
	info, _ := debug.ReadBuildInfo()
	return versionOf(version, info)
}()

// versionOf is linked unless it is dev and info names the commit, which it shortens as git describe does, -dirty if modified.
func versionOf(linked string, info *debug.BuildInfo) string {
	if linked != "dev" || info == nil {
		return linked
	}
	var commit string
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value[:min(len(s.Value), shortCommit)]
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	switch {
	case commit == "":
		return linked
	case dirty:
		return commit + "-dirty"
	default:
		return commit
	}
}

// shortCommit is how much of a commit names it, as the Makefile's git describe --abbrev gives it.
const shortCommit = 12

// Build adds a binary's modules to the host from cfg.
type Build[C Config] func(ctx context.Context, h *Host, cfg C) error

// Main runs Start until SIGINT or SIGTERM (a second one kills the process), or with healthcheckArg probes the process.
func Main[C Config](name string, build Build[C]) int {
	if slices.Equal(os.Args[1:], []string{healthcheckArg}) {
		ctx, cancel := context.WithTimeout(context.Background(), healthcheckTimeout)
		defer cancel()
		return Healthcheck[C](ctx, os.Stderr, name)
	}
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// The shutdown starts only once the handler is gone, so the next signal gets the default action.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	context.AfterFunc(signalCtx, func() { stop(); cancel() })
	return Start(ctx, os.Stdout, os.Stderr, name, build)
}

// Start loads the configuration, builds the process's host and modules and runs it until ctx ends; config errors go to stderr.
func Start[C Config](ctx context.Context, stdout, stderr io.Writer, name string, build Build[C]) int {
	cfg, err := LoadConfig[C](ConfigDir)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, name+": invalid configuration:", err)
		return 1
	}
	settings, err := (*cfg).Host(name)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, name+": invalid configuration:", err)
		return 1
	}
	log, err := logger.New(stdout, settings.Log)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, name+":", err)
		return 1
	}
	h, err := New(Options{Name: name, Settings: settings, Logger: log, Registry: metrics.NewRegistry()})
	if err != nil {
		log.ErrorContext(ctx, "startup failed", "service", name, "err", err)
		return 1
	}
	defer h.Close()
	buildCtx, cancel := context.WithTimeout(ctx, buildTimeout)
	defer cancel()
	if err := build(buildCtx, h, *cfg); err != nil {
		log.ErrorContext(ctx, "startup failed", "service", name, "err", err)
		return 1
	}
	log.InfoContext(ctx, "starting", "service", name, "version", buildVersion)
	if err := h.Run(ctx); err != nil {
		log.ErrorContext(ctx, "stopped with an error", "service", name, "err", err)
		return 1
	}
	log.InfoContext(ctx, "stopped", "service", name)
	return 0
}

// LoadConfig reads the configuration from dir under where the binary runs, or CONFIG_PATH, with FINGA_ overrides; a key no field names is an error.
func LoadConfig[C any](dir string) (*C, error) {
	return configload.Load[C](configload.Options{Path: configload.ResolvePath(dir), EnvPrefix: EnvPrefix, Strict: true})
}
