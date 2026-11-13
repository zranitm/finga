package host_test

import (
	"bytes"
	"context"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/sdk/freeport"

	"finga/internal/host"
	"finga/pkg/observability/logger"
	"finga/pkg/observability/metrics"
	"finga/pkg/runner/runnertest"
)

// edgeConfig is a made-up service that serves no gRPC, as the gateway.
type edgeConfig struct {
	Ops           host.OpsSection `mapstructure:"ops"`
	ShutdownGrace time.Duration   `mapstructure:"shutdown_grace" validate:"gt=0"`
	Log           logger.Config   `mapstructure:"log"`
}

func (c edgeConfig) Host() host.Settings {
	return host.Settings{Ops: c.Ops.Addr, ShutdownGrace: c.ShutdownGrace, Log: c.Log}
}

// The probe a container runs: the service's gRPC health, or its readiness when it serves no gRPC.
func TestHealthcheck(t *testing.T) {
	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	log := runnertest.NewLog()
	shop := newHost(t, host.Options{Name: "shop", Settings: host.Settings{GRPC: "127.0.0.1:0", Ops: "127.0.0.1:0", ShutdownGrace: time.Second}, Logger: log.Logger(), Registry: metrics.NewRegistry()})
	grpcAddr := runnertest.Start(t, log, shop.Run, "grpc", "ops").Addr("grpc")
	edgeLog := runnertest.NewLog()
	opsAddr := runnertest.Start(t, edgeLog, newPublic(t, edgeLog).Run, "http", "ops").Addr("ops")
	_, port, err := net.SplitHostPort(grpcAddr)
	if err != nil {
		t.Fatal(err)
	}
	// Held for the test and never bound; a port the kernel picked and freed could be bound again by another test meanwhile.
	closed := net.JoinHostPort("127.0.0.1", strconv.Itoa(freeport.GetOne(t)))

	tests := []struct {
		name string
		edge bool
		env  map[string]string
		want int
		out  string
	}{
		{"a serving gRPC service is healthy", false, map[string]string{"FINGA_GRPC_LISTEN": grpcAddr}, 0, ""},
		{"a wildcard listen address is probed on loopback", false, map[string]string{"FINGA_GRPC_LISTEN": ":" + port}, 0, ""},
		{"a gRPC service that is not listening is not", false, map[string]string{"FINGA_GRPC_LISTEN": closed}, 1, "shop: unhealthy"},
		{"a service without gRPC is healthy while ready", true, map[string]string{"FINGA_OPS_ADDR": opsAddr}, 0, ""},
		{"a service without gRPC that is not listening is not", true, map[string]string{"FINGA_OPS_ADDR": closed}, 1, "edge: unhealthy"},
		{"a configuration error is not", false, map[string]string{"FINGA_STOCK": "0"}, 1, "invalid configuration"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(testdata)
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var out bytes.Buffer
			code := host.Healthcheck[shopConfig](ctx, &out, "shop")
			if tt.edge {
				code = host.Healthcheck[edgeConfig](ctx, &out, "edge")
			}
			if code != tt.want || !strings.Contains(out.String(), tt.out) {
				t.Fatalf("code %d, output %q; want %d and %q", code, out.String(), tt.want, tt.out)
			}
		})
	}
}
