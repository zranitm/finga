// Package grpctest serves a gRPC server over an in-memory connection for tests.
package grpctest

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"finga/pkg/grpcx"
)

const bufSize = 1 << 20

// Conn serves srv until the test ends and returns a grpcx.Dial client connection to it.
func Conn(t testing.TB, srv *grpc.Server) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpcx.Dial("passthrough:///bufconn", grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
		srv.Stop()
	})
	return conn
}
