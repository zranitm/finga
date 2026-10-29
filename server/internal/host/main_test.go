package host_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"finga/internal/host"
)

// shopConfig is a made-up service's own configuration, declared as every
// service declares its own: the host's sections and a key of its own.
type shopConfig struct {
	GRPC          host.GRPCSection `mapstructure:"grpc"`
	Ops           host.OpsSection  `mapstructure:"ops"`
	ShutdownGrace time.Duration    `mapstructure:"shutdown_grace" validate:"gt=0"`
	LogLevel      string           `mapstructure:"log_level" validate:"oneof=debug info warn error"`
	Stock         int              `mapstructure:"stock" validate:"min=1"`
}

func (c shopConfig) Host() host.Settings {
	return host.Settings{GRPC: c.GRPC.Listen, Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, LogLevel: c.LogLevel}
}

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
	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		dir      string // where the binary runs; Start reads services/shop/config/ from there
		env      map[string]string
		build    func(*process) host.Build[shopConfig]
		runErr   error
		wantCode int
		wantLog  string
		closed   bool
	}{
		{"reads the service's own file, builds, runs until the context ends, and closes", testdata, nil, ok, nil, 0, `"msg":"stopped"`, true},
		{"an override reaches the service's own key", testdata, map[string]string{"FINGA_STOCK": "7"}, wantStock(7), nil, 0, `"msg":"stopped"`, true},
		{"a configuration error stops it before the build", testdata, map[string]string{"FINGA_STOCK": "0"}, ok, nil, 1, "invalid configuration", false},
		{"a bad log level stops it", testdata, map[string]string{"FINGA_LOG_LEVEL": "loud"}, ok, nil, 1, "log_level", false},
		{"a missing file is named in the error", t.TempDir(), nil, ok, nil, 1, filepath.Join("services", "shop", "config", "config.yaml"), false},
		{"a failed build", testdata, nil, func(*process) host.Build[shopConfig] {
			return func(context.Context, shopConfig, *slog.Logger, *prometheus.Registry) (host.Process, error) {
				return nil, errors.New("no database")
			}
		}, nil, 1, "no database", false},
		{"a run that fails", testdata, nil, ok, errors.New("listener died"), 1, "listener died", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(tt.dir)
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

// ok builds when it got the service's own file (stock 3) and a registry.
func ok(p *process) host.Build[shopConfig] { return wantStock(3)(p) }

func wantStock(stock int) func(*process) host.Build[shopConfig] {
	return func(p *process) host.Build[shopConfig] {
		return func(_ context.Context, cfg shopConfig, _ *slog.Logger, reg *prometheus.Registry) (host.Process, error) {
			if cfg.Stock != stock || cfg.GRPC.Listen == "" || reg == nil {
				return nil, errors.New("the build got the wrong configuration or no registry")
			}
			return p, nil
		}
	}
}
