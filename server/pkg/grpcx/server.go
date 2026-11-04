package grpcx

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"finga/pkg/panics"
)

// ServerOptions configure NewServer.
type ServerOptions struct {
	Errors ErrorTable
	// Logger records unexplained errors; nil discards them.
	Logger *slog.Logger
	// Panics records a recovered panic; nil logs it to Logger.
	Panics *panics.Recorder
	// Draining, once closed, ends every open stream with Unavailable, so GracefulStop need not wait for
	// streams that never end on their own; their clients resubscribe. Nil never drains.
	Draining <-chan struct{}
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
		grpc.ChainUnaryInterceptor(i.unary),
		grpc.ChainStreamInterceptor(i.stream),
	}, extra...)
	return grpc.NewServer(all...)
}

type interceptors struct{ opts ServerOptions }

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
		i.opts.Logger.ErrorContext(ctx, "rpc failed", "method", method, "request_id", RequestID(ctx), "err", err)
	}
	return st.Err()
}

func (i interceptors) recover(ctx context.Context, method string, err *error) {
	if r := recover(); r != nil {
		i.opts.Panics.Record(ctx, panics.FlowGRPC, panics.Of(r), "method", method, "request_id", RequestID(ctx))
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
			return WithRequestID(ctx, ids[0])
		}
	}
	return ctx
}
