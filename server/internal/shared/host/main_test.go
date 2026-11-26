package host_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"finga/internal/shared/host"
	"finga/pkg/observability/logger"
	"finga/pkg/observability/tracing"
)

// mallConfig is a made-up server's configuration: the shared sections, then a section per module, each run as its own process.
type mallConfig struct {
	ShutdownGrace time.Duration  `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log           logger.Config  `mapstructure:"log"`
	Tracing       tracing.Config `mapstructure:"tracing"`
	Shop          shopSection    `mapstructure:"shop"`
	Edge          edgeSection    `mapstructure:"edge"`
}

type shopSection struct {
	Ops   host.OpsSection `mapstructure:"ops"`
	Stock int             `mapstructure:"stock" validate:"min=1"`
}

type edgeSection struct {
	Ops host.OpsSection `mapstructure:"ops"`
}

func (c mallConfig) Host(process string) (host.Settings, error) {
	s := host.Settings{ShutdownGrace: c.ShutdownGrace, Log: c.Log, Tracing: c.Tracing}
	switch process {
	case "shop":
		s.Ops = c.Shop.Ops.Addr
	case "edge":
		s.Ops = c.Edge.Ops.Addr
	default:
		return host.Settings{}, fmt.Errorf("no process %s", process)
	}
	return s, nil
}

func TestStart(t *testing.T) {
	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name     string
		dir      string // where the binary runs; Start reads config/ from there
		process  string
		env      map[string]string
		build    func(closed *bool) host.Build[mallConfig]
		wantCode int
		wantLog  string
		closed   bool
	}{
		{"reads the file, builds, runs until the context ends, and closes", testdata, "shop", nil, wantStock(3), 0, `"msg":"stopped"`, true},
		{"an override reaches a module's own key", testdata, "shop", map[string]string{"FINGA_SHOP_STOCK": "7"}, wantStock(7), 0, `"msg":"stopped"`, true},
		{"a configuration error stops it before the build", testdata, "shop", map[string]string{"FINGA_SHOP_STOCK": "0"}, wantStock(3), 1, "invalid configuration", false},
		{"a bad log level stops it", testdata, "shop", map[string]string{"FINGA_LOG_LEVEL": "loud"}, wantStock(3), 1, "log.level", false},
		{"a variable that names no key is warned about, not refused", testdata, "shop", map[string]string{"FINGA_SHOP_STOK": "7"}, wantStock(3), 0, `"var":"FINGA_SHOP_STOK"`, true},
		{"a key no field names stops it, as every binary loads strictly", filepath.Join(testdata, "unknown-key"), "shop", nil, wantStock(3), 1, "colour", false},
		{"a missing file is named in the error", t.TempDir(), "shop", nil, wantStock(3), 1, filepath.Join("config", "config.yaml"), false},
		{"a process the file has no settings for", testdata, "kiosk", nil, wantStock(3), 1, "no process kiosk", false},
		{"a failed build", testdata, "shop", nil, func(closed *bool) host.Build[mallConfig] {
			return func(_ context.Context, h *host.Host, _ mallConfig) error {
				h.OnClose(func() { *closed = true })
				return errors.New("no database")
			}
		}, 1, "no database", true},
		{"a run that fails", testdata, "shop", nil, func(closed *bool) host.Build[mallConfig] {
			return func(_ context.Context, h *host.Host, _ mallConfig) error {
				h.OnClose(func() { *closed = true })
				m, err := h.Module("shop")
				if err != nil {
					return err
				}
				m.Task("till", func(context.Context) error { return errors.New("till died") }, host.Vital)
				return nil
			}
		}, 1, "till died", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(tt.dir)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			closed := false
			var out lockedBuffer // an earlier test's exporter may still write its error to the process's one handler, which is this host's logger now
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan int, 1)
			go func() { done <- host.Start(ctx, &out, &out, tt.process, tt.build(&closed)) }()
			if tt.wantCode == 0 {
				cancel()
			}
			var code int
			select {
			case code = <-done:
			case <-time.After(5 * time.Second): // it should have stopped by itself: stop it, and see why below
				cancel()
				code = <-done
			}
			cancel()
			if code != tt.wantCode || !strings.Contains(out.String(), tt.wantLog) || closed != tt.closed {
				t.Fatalf("code %d, closed %v, output %s", code, closed, out.String())
			}
		})
	}
}

// However a start fails, the tracer provider's batch processor, which Start makes with the host, is shut down: no exporter goroutine is left running.
func TestAFailedStartLeavesNoExporterRunning(t *testing.T) {
	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(testdata)
	t.Setenv("FINGA_TRACING_OTLP_ENDPOINT", "127.0.0.1:1") // nothing listens
	for _, c := range []struct {
		name  string
		env   map[string]string
		build host.Build[mallConfig]
		code  int
	}{
		{"a failed build", nil, func(context.Context, *host.Host, mallConfig) error { return errors.New("no database") }, 1},
		{"a bad log level", map[string]string{"FINGA_LOG_LEVEL": "loud"}, func(context.Context, *host.Host, mallConfig) error { return nil }, 1},
		{"a build that works, then the context ends", nil, func(context.Context, *host.Host, mallConfig) error { return nil }, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			ctx, cancel := context.WithCancel(context.Background())
			if c.code == 0 {
				cancel()
			}
			defer cancel()
			var out lockedBuffer
			before := exporters() // a test before may have left one
			if code := host.Start(ctx, &out, &out, "shop", c.build); code != c.code {
				t.Fatalf("code %d, want %d, output %s", code, c.code, out.String())
			}
			deadline := time.Now().Add(5 * time.Second)
			for exporters() > before {
				if time.Now().After(deadline) {
					t.Fatalf("%d batch processors running, %d before Start, after it returned %d", exporters(), before, c.code)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}

// exporters is how many tracer providers' batch processors run: their goroutines.
func exporters() int {
	buf := make([]byte, 1<<20)
	return strings.Count(string(buf[:runtime.Stack(buf, true)]), "(*batchSpanProcessor).processQueue") + strings.Count(string(buf[:runtime.Stack(buf, true)]), "NewBatchSpanProcessor.func")
}

// wantStock builds when it got the shop's stock and the process's settings, and marks the host closed.
func wantStock(stock int) func(*bool) host.Build[mallConfig] {
	return func(closed *bool) host.Build[mallConfig] {
		return func(_ context.Context, h *host.Host, cfg mallConfig) error {
			h.OnClose(func() { *closed = true })
			if cfg.Shop.Stock != stock || h.Registry() == nil {
				return errors.New("the build got the wrong configuration or no registry")
			}
			return nil
		}
	}
}
