package grpcx

import (
	"context"
	"errors"
	"log/slog"
	"time"

	grpcprom "github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/status"

	"finga/pkg/logger"
	"finga/pkg/panics"
)

// ServerOptions configure NewServer.
type ServerOptions struct {
	Errors ErrorTable
	// Logger records unexplained errors, and a sample of the Unavailable ones with their cause; nil discards them.
	Logger *slog.Logger
	// Panics records a recovered panic; nil logs it to Logger.
	Panics *panics.Recorder
	// Metrics count and time every call by service, method and code, as the caller sees it; nil records none.
	Metrics *grpcprom.ServerMetrics
	// Draining, once closed, ends every open stream with Unavailable so GracefulStop need not wait; nil never drains.
	Draining <-chan struct{}
	// Tracing gives calls spans under the caller's trace; nil traces none.
	Tracing *Tracing
}

// NewServer is a gRPC server that recovers panics, maps errors, and keeps its clients' connections alive as Dial's do.
func NewServer(opts ServerOptions, extra ...grpc.ServerOption) *grpc.Server {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	if opts.Panics == nil {
		opts.Panics = panics.NewRecorder(opts.Logger, nil)
	}
	i := interceptors{opts: opts, unavailable: logger.NewSampler(unavailableLogGap)}
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

// NewServerMetrics are the grpc_server_* metrics; InitializeMetrics, once services are registered, starts each at 0.
func NewServerMetrics() *grpcprom.ServerMetrics {
	return grpcprom.NewServerMetrics(grpcprom.WithServerHandlingTimeHistogram(grpcprom.WithHistogramBuckets(rpcBuckets)))
}

// NewClientMetrics are the grpc_client_* counters and handling-time histogram by service and method, for ClientMetrics.
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

// unavailableLogGap spaces each method's lines for a dependency out of reach: in an outage, every call fails.
const unavailableLogGap = 10 * time.Second

type interceptors struct {
	opts        ServerOptions
	unavailable *logger.Sampler // by method
}

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
	defer i.recover(ctx, info.FullMethod, &err)
	resp, err = handler(ctx, req)
	return resp, i.status(ctx, info.FullMethod, err)
}

var errDraining = errors.New("server draining")

func (i interceptors) stream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	ctx, cancel := context.WithCancelCause(ss.Context())
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
	switch {
	case !explained:
		i.opts.Logger.ErrorContext(ctx, "rpc failed", "method", method, "err", err)
	case outOfReach(st):
		i.unavailable.Do(method, func(suppressed int) {
			i.opts.Logger.WarnContext(ctx, "rpc unavailable", "method", method, "err", err, "suppressed", suppressed)
		})
	}
	return st.Err()
}

// outOfReach reports whether st is Unavailable with no reason: a dependency busy or out of reach, whose cause only this service's log names.
func outOfReach(st *status.Status) bool {
	_, rule := reasonIn(st)
	return st.Code() == codes.Unavailable && !rule
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

// Context returns the stream's context, which a drain cancels.
func (s *contextStream) Context() context.Context { return s.ctx }
