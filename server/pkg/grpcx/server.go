package grpcx

import (
	"context"
	"errors"
	"log/slog"

	grpcprom "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"finga/pkg/panics"
	"finga/pkg/reqid"
)

// ServerOptions configure NewServer.
type ServerOptions struct {
	Errors ErrorTable
	// Logger records unexplained errors; nil discards them.
	Logger *slog.Logger
	// Panics records a recovered panic; nil logs it to Logger.
	Panics *panics.Recorder
	// Metrics count and time every call by service, method and code, as the caller sees it; nil records none.
	Metrics *grpcprom.ServerMetrics
	// Draining, once closed, ends every open stream with Unavailable, so GracefulStop need not wait for
	// streams that never end on their own; their clients resubscribe. Nil never drains.
	Draining <-chan struct{}
	// Tracing gives calls spans under the caller's trace; nil traces none.
	Tracing *Tracing
}

// NewServer is a gRPC server that propagates request IDs, recovers panics, maps errors, and keeps its
// clients' connections alive as Dial's do.
func NewServer(opts ServerOptions, extra ...grpc.ServerOption) *grpc.Server {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Panics == nil {
		opts.Panics = panics.NewRecorder(opts.Logger, nil)
	}
	i := interceptors{opts: opts}
	all := append([]grpc.ServerOption{
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{MinTime: keepaliveMinTime, PermitWithoutStream: true}),
		grpc.KeepaliveParams(keepalive.ServerParameters{Time: keepaliveTime, Timeout: keepaliveTimeout}),
		grpc.ChainUnaryInterceptor(i.unaries()...),
		grpc.ChainStreamInterceptor(i.streams()...),
	}, extra...)
	if opts.Tracing != nil {
		all = append(all, opts.Tracing.serverOption())
	}
	return grpc.NewServer(all...)
}

// rpcBuckets span a sub-millisecond world command to a slow database call, in seconds.
var rpcBuckets = []float64{.0002, .0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5}

// NewServerMetrics are grpc_server_started_total, grpc_server_handled_total and grpc_server_handling_seconds
// by service and method (handled by code too); register them, pass them in ServerOptions, then call
// InitializeMetrics once the services are registered, so every method's series starts at 0.
func NewServerMetrics() *grpcprom.ServerMetrics {
	return grpcprom.NewServerMetrics(grpcprom.WithServerHandlingTimeHistogram(grpcprom.WithHistogramBuckets(rpcBuckets)))
}

// NewClientMetrics are grpc_client_started_total, grpc_client_handled_total and grpc_client_handling_seconds
// by service and method (handled by code too), for ClientMetrics.
func NewClientMetrics() *grpcprom.ClientMetrics {
	return grpcprom.NewClientMetrics(grpcprom.WithClientHandlingTimeHistogram(grpcprom.WithHistogramBuckets(rpcBuckets)))
}

// ClientMetrics are the Dial options that count and time a connection's calls in m.
func ClientMetrics(m *grpcprom.ClientMetrics) []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithChainUnaryInterceptor(m.UnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(m.StreamClientInterceptor()),
	}
}

type interceptors struct{ opts ServerOptions }

// unaries and streams put the metrics outermost, so they count the code the error table gave.
func (i interceptors) unaries() []grpc.UnaryServerInterceptor {
	if i.opts.Metrics == nil {
		return []grpc.UnaryServerInterceptor{i.unary}
	}
	return []grpc.UnaryServerInterceptor{i.opts.Metrics.UnaryServerInterceptor(), i.unary}
}

func (i interceptors) streams() []grpc.StreamServerInterceptor {
	if i.opts.Metrics == nil {
		return []grpc.StreamServerInterceptor{i.stream}
	}
	return []grpc.StreamServerInterceptor{i.opts.Metrics.StreamServerInterceptor(), i.stream}
}

func (i interceptors) unary(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
	ctx = requestIDFromIncoming(ctx)
	defer i.recover(ctx, info.FullMethod, &err)
	resp, err = handler(ctx, req)
	return resp, i.status(ctx, info.FullMethod, err)
}

var errDraining = errors.New("server draining")

func (i interceptors) stream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	ctx, cancel := context.WithCancelCause(requestIDFromIncoming(ss.Context()))
	defer cancel(nil)
	defer i.endOnDrain(cancel)()
	defer i.recover(ctx, info.FullMethod, &err)
	err = handler(srv, &contextStream{ServerStream: ss, ctx: ctx})
	if errors.Is(context.Cause(ctx), errDraining) {
		return status.Error(codes.Unavailable, "server shutting down")
	}
	return i.status(ctx, info.FullMethod, err)
}

// endOnDrain cancels a stream's context once the server drains, until the returned stop.
func (i interceptors) endOnDrain(cancel context.CancelCauseFunc) (stop func()) {
	if i.opts.Draining == nil {
		return func() {}
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-i.opts.Draining:
			cancel(errDraining)
		case <-done:
		}
	}()
	return func() { close(done) }
}

func (i interceptors) status(ctx context.Context, method string, err error) error {
	if err == nil {
		return nil
	}
	st, explained := i.opts.Errors.Status(err)
	if !explained {
		i.opts.Logger.ErrorContext(ctx, "rpc failed", "method", method, "err", err)
	}
	return st.Err()
}

func (i interceptors) recover(ctx context.Context, method string, err *error) {
	if r := recover(); r != nil {
		i.opts.Panics.Record(ctx, panics.FlowGRPC, panics.Of(r), "method", method)
		*err = status.Error(codes.Internal, internalMessage)
	}
}

type contextStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context returns the context carrying the request ID.
func (s *contextStream) Context() context.Context { return s.ctx }

func requestIDFromIncoming(ctx context.Context) context.Context {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if ids := md.Get(RequestIDHeader); len(ids) > 0 && ids[0] != "" {
			return reqid.With(ctx, ids[0])
		}
	}
	return ctx
}
