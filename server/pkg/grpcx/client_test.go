package grpcx_test

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"

	"finga/pkg/grpcx"
)

// serveHealth serves the health service on addr until the test ends or stop is called.
func serveHealth(t *testing.T, addr string) (stop func()) {
	t.Helper()
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	healthpb.RegisterHealthServer(srv, health.NewServer())
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return srv.Stop
}

// A dependency's container restarting has no DNS record while it is down. Its name is looked up again at each connect, and the connect is tried at least every few seconds, so it is reached within seconds of being back.
func TestADependencyBackFromARestartIsReachedWithinSeconds(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	_ = lis.Close()
	stop := serveHealth(t, addr)
	var (
		down     atomic.Bool
		mu       sync.Mutex
		attempts []time.Time
	)
	// As the container's DNS answers: no such host while it is down, its address once back.
	dialer := func(ctx context.Context, target string) (net.Conn, error) {
		if target != "item.test:9003" {
			t.Errorf("dialed %q, want the name as given", target)
		}
		mu.Lock()
		attempts = append(attempts, time.Now())
		mu.Unlock()
		if down.Load() {
			return nil, &net.DNSError{Err: "no such host", Name: "item.test", IsNotFound: true}
		}
		var d net.Dialer
		return d.DialContext(ctx, "tcp", addr)
	}
	conn, err := grpcx.Dial("item.test:9003", grpc.WithContextDialer(dialer))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := healthpb.NewHealthClient(conn)
	check := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		_, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
		return err
	}
	if err := check(); err != nil {
		t.Fatalf("before the restart: %v", err)
	}

	down.Store(true)
	stop()
	downAt := time.Now()
	// Long enough that gRPC's default backoff would wait over 5 s between two tries.
	for until := downAt.Add(12 * time.Second); time.Now().Before(until); time.Sleep(200 * time.Millisecond) {
		if check() == nil {
			t.Fatal("a call passed while the dependency was down")
		}
	}
	serveHealth(t, addr)
	down.Store(false)
	back := time.Now()
	for check() != nil {
		if time.Since(back) > 4*time.Second {
			t.Fatalf("still failing %s after the dependency came back", time.Since(back))
		}
		time.Sleep(100 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	last := downAt
	for _, at := range attempts {
		if at.Before(downAt) {
			continue
		}
		if gap := at.Sub(last); gap > 4*time.Second {
			t.Fatalf("%s between two connects while the dependency was down", gap)
		}
		last = at
	}
}
