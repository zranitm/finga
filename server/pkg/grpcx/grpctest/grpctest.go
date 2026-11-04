// Package grpctest serves a gRPC server over an in-memory connection for tests, and builds the failures services send.
package grpctest

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protoreflect"

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

// Error is a failure carrying reason, as a service's error table sends it.
func Error(code codes.Code, reason protoreflect.Enum) error {
	return grpcx.ErrorFor(code, grpcx.Key(reason))
}

// Reasons lists a reason enum's non-zero values in order: what a calling side's table must cover.
func Reasons(d protoreflect.EnumDescriptor) []grpcx.Reason {
	values := d.Values()
	out := make([]grpcx.Reason, 0, values.Len())
	for i := range values.Len() {
		v := values.Get(i)
		if v.Number() == 0 {
			continue
		}
		out = append(out, grpcx.Reason{Domain: string(d.ParentFile().Package()), Name: string(v.Name())})
	}
	return out
}
