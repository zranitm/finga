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
	"finga/pkg/observability/metrics"
	"finga/pkg/runner/runnertest"
)

// The probe a container runs: the service's gRPC health, or its readiness when it serves no gRPC.
func TestHealthcheck(t *testing.T) {
	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	log := runnertest.NewLog()
	shop := newHost(t, host.Options{Name: "shop", Settings: host.Settings{GRPC: "127.0.0.1:0", Ops: "127.0.0.1:0", ShutdownGrace: time.Second}, Logger: log.Logger(), Registry: metrics.NewRegistry()})
	serveGRPC(t, shop, "shop")
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
		name    string
		process string
		env     map[string]string
		want    int
		out     string
	}{
		{"a serving gRPC process is healthy", "shop", map[string]string{"FINGA_SHOP_GRPC_LISTEN": grpcAddr}, 0, ""},
		{"a wildcard listen address is probed on loopback", "shop", map[string]string{"FINGA_SHOP_GRPC_LISTEN": ":" + port}, 0, ""},
		{"a gRPC process that is not listening is not", "shop", map[string]string{"FINGA_SHOP_GRPC_LISTEN": closed}, 1, "shop: unhealthy"},
		{"a process without gRPC is healthy while ready", "edge", map[string]string{"FINGA_EDGE_OPS_ADDR": opsAddr}, 0, ""},
		{"a process without gRPC that is not listening is not", "edge", map[string]string{"FINGA_EDGE_OPS_ADDR": closed}, 1, "edge: unhealthy"},
		{"a configuration error is not", "shop", map[string]string{"FINGA_SHOP_STOCK": "0"}, 1, "invalid configuration"},
		{"a process the file has no settings for is not", "kiosk", nil, 1, "no process kiosk"},
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
			code := host.Healthcheck[mallConfig](ctx, &out, tt.process)
			if code != tt.want || !strings.Contains(out.String(), tt.out) {
				t.Fatalf("code %d, output %q; want %d and %q", code, out.String(), tt.want, tt.out)
			}
		})
	}
}
