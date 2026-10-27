package host_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/config"
	"finga/internal/host"
)

type process struct {
	runErr error
	closed bool
}

func (p *process) Run(ctx context.Context) error {
	if p.runErr != nil {
		return p.runErr
	}
	<-ctx.Done()
	return nil
}

func (p *process) Close() { p.closed = true }

func TestStart(t *testing.T) {
	t.Setenv("CONFIG_PATH", "../../configs/config.yaml")
	tests := []struct {
		name     string
		env      map[string]string
		build    func(*process) host.Build
		runErr   error
		wantCode int
		wantLog  string
		closed   bool
	}{
		{"builds, runs until the context ends, and closes", nil, ok, nil, 0, `"msg":"stopped"`, true},
		{"a configuration error stops it before the build", map[string]string{"FINGA_WORLD_TICK_RATE_HZ": "25"}, ok, nil, 1, "invalid configuration", false},
		{"a bad log level stops it", map[string]string{"FINGA_TELEMETRY_LOG_LEVEL": "loud"}, ok, nil, 1, "log_level", false},
		{"a failed build", nil, func(*process) host.Build {
			return func(context.Context, config.Config, *slog.Logger, *prometheus.Registry) (host.Process, error) {
				return nil, errors.New("no database")
			}
		}, nil, 1, "no database", false},
		{"a run that fails", nil, ok, errors.New("listener died"), 1, "listener died", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			p := &process{runErr: tt.runErr}
			var out bytes.Buffer
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan int, 1)
			go func() { done <- host.Start(ctx, &out, &out, "shop", "v1", tt.build(p)) }()
			if tt.wantCode == 0 {
				cancel()
			}
			code := <-done
			cancel()
			if code != tt.wantCode || !strings.Contains(out.String(), tt.wantLog) || p.closed != tt.closed {
				t.Fatalf("code %d, closed %v, output %s", code, p.closed, out.String())
			}
		})
	}
}

func ok(p *process) host.Build {
	return func(_ context.Context, cfg config.Config, _ *slog.Logger, reg *prometheus.Registry) (host.Process, error) {
		if cfg.Services.Item.Listen == "" || reg == nil {
			return nil, errors.New("the build got no configuration or registry")
		}
		return p, nil
	}
}
