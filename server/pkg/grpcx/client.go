package grpcx

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// Keepalive: a client pings an idle connection every keepaliveTime and drops it when no answer comes
// within keepaliveTimeout, so a half-open link on a long-lived stream breaks in seconds, not minutes.
// A server accepts pings no more often than keepaliveMinTime.
const (
	keepaliveTime    = 20 * time.Second
	keepaliveTimeout = 5 * time.Second
	keepaliveMinTime = 10 * time.Second
)

// RequestIDHeader is the metadata key the request ID travels under.
const RequestIDHeader = "x-request-id"

type requestIDKey struct{}

// WithRequestID returns ctx carrying id; a call made with it sends the ID.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID is the request ID in ctx, or empty.
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

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
	if id := RequestID(ctx); id != "" {
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
