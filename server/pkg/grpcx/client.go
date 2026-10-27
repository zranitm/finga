package grpcx

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
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

// Dial makes a client connection to addr (host:port, or any target
// grpc.NewClient takes). It connects lazily and reconnects on its own. The
// transport is plain-text: in Stage 1 the services trust each other on the
// internal network, and mTLS comes later. Calls send the request ID of
// their context.
func Dial(addr string, extra ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts := append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
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
