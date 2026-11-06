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
	"google.golang.org/protobuf/types/descriptorpb"

	"finga/pkg/grpcx"
)

const bufSize = 1 << 20

// Conn serves srv until the test ends and returns a grpcx.Dial client connection to it, with extra.
func Conn(t testing.TB, srv *grpc.Server, extra ...grpc.DialOption) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(bufSize)
	go func() { _ = srv.Serve(lis) }()
	conn, err := grpcx.Dial("passthrough:///bufconn", append([]grpc.DialOption{grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
		return lis.DialContext(ctx)
	})}, extra...)...)
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

// Reasons lists the reasons a service still sends, in order: every value of its enum but the zero one and
// the deprecated ones. Its own table and its callers' must cover them all.
func Reasons(d protoreflect.EnumDescriptor) []grpcx.Reason {
	values := d.Values()
	out := make([]grpcx.Reason, 0, values.Len())
	for i := range values.Len() {
		v := values.Get(i)
		if v.Number() == 0 || deprecated(v) {
			continue
		}
		out = append(out, grpcx.Reason{Domain: string(d.ParentFile().Package()), Name: string(v.Name())})
	}
	return out
}

func deprecated(v protoreflect.EnumValueDescriptor) bool {
	opts, ok := v.Options().(*descriptorpb.EnumValueOptions)
	return ok && opts.GetDeprecated()
}
