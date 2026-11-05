package grpcx

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"

	"finga/pkg/reqid"
)

// Keepalive: a client pings an idle connection every keepaliveTime and drops it when no answer comes
// within keepaliveTimeout, so a half-open link on a long-lived stream breaks in seconds, not minutes.
// A server accepts pings no more often than keepaliveMinTime.
const (
	keepaliveTime    = 20 * time.Second
	keepaliveTimeout = 5 * time.Second
	keepaliveMinTime = 10 * time.Second
)

// RequestIDHeader is the metadata key the request ID (package reqid) travels under.
const RequestIDHeader = "x-request-id"

// Dial makes a plain-text (internal network) client connection that sends the request ID.
func Dial(addr string, extra ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: keepaliveTime, Timeout: keepaliveTimeout, PermitWithoutStream: true}),
		grpc.WithChainUnaryInterceptor(sendRequestIDUnary),
		grpc.WithChainStreamInterceptor(sendRequestIDStream),
	}, extra...)
	return grpc.NewClient(addr, opts...)
}

func outgoing(ctx context.Context) context.Context {
	if id := reqid.From(ctx); id != "" {
		return metadata.AppendToOutgoingContext(ctx, RequestIDHeader, id)
	}
	return ctx
}

func sendRequestIDUnary(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	return invoker(outgoing(ctx), method, req, reply, cc, opts...)
}

func sendRequestIDStream(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return streamer(outgoing(ctx), desc, cc, method, opts...)
}
