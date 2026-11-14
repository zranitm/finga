package host

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
)

// healthcheckArg makes a binary probe its running service instead of serving, as a distroless image has no probe tool.
const healthcheckArg = "-healthcheck"

// Healthcheck probes the readiness of the process beside it; 0 means it serves.
func Healthcheck[C Config](ctx context.Context, stderr io.Writer, name string) int {
	cfg, err := LoadConfig[C](ConfigDir)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, name+": invalid configuration:", err)
		return 1
	}
	s, err := (*cfg).Host(name)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, name+": invalid configuration:", err)
		return 1
	}
	if err := checkReady(ctx, loopback(s.Ops)); err != nil {
		_, _ = fmt.Fprintln(stderr, name+": unhealthy:", err)
		return 1
	}
	return 0
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
