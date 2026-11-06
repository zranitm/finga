package host

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// healthcheckArg makes a binary probe its running service instead of serving, as a distroless image has no probe tool.
const healthcheckArg = "-healthcheck"

// Healthcheck probes the service running beside it, configured as it is: its gRPC health when it serves
// gRPC, else its readiness. It returns 0 while the service serves.
func Healthcheck[C Config](ctx context.Context, stderr io.Writer, name string) int {
	cfg, err := loadConfig[C](name)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, name+": invalid configuration:", err)
		return 1
	}
	if s := (*cfg).Host(); s.GRPC != "" {
		err = checkGRPC(ctx, loopback(s.GRPC))
	} else {
		err = checkReady(ctx, loopback(s.Ops))
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, name+": unhealthy:", err)
		return 1
	}
	return 0
}

func checkGRPC(ctx context.Context, addr string) error {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	res, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return err
	}
	if res.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return fmt.Errorf("gRPC health is %s", res.GetStatus())
	}
	return nil
}

func checkReady(ctx context.Context, addr string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/readyz", nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("/readyz answered %s", res.Status)
	}
	return nil
}

// loopback is where a probe beside the service reaches a listen address: an unspecified host is this one.
func loopback(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || (host != "" && !net.ParseIP(host).IsUnspecified()) {
		return addr
	}
	return net.JoinHostPort("127.0.0.1", port)
}
