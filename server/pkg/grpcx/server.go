package grpcx

import (
	"context"
	"log/slog"
	"runtime/debug"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ServerOptions configure NewServer.
type ServerOptions struct {
	// Errors turns a handler's error into its status (ErrorTable.Status).
	Errors ErrorTable
	// Logger records a panic, and an error the table did not explain, with
	// its cause; the caller only sees Internal. nil discards them.
	Logger *slog.Logger
}

// NewServer is a gRPC server whose handlers, unary and streaming, run with
// the caller's request ID in their context, have a panic answered as
// Internal, and have every error turned into a status through the table.
func NewServer(opts ServerOptions, extra ...grpc.ServerOption) *grpc.Server {
	if opts.Logger == nil {
		opts.Logger = slog.New(slog.DiscardHandler)
	}
	i := interceptors{opts: opts}
	all := append([]grpc.ServerOption{
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

func (i interceptors) stream(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
	ctx := requestIDFromIncoming(ss.Context())
	defer i.recover(ctx, info.FullMethod, &err)
	err = handler(srv, &contextStream{ServerStream: ss, ctx: ctx})
	return i.status(ctx, info.FullMethod, err)
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
		i.opts.Logger.ErrorContext(ctx, "rpc panicked", "method", method, "request_id", RequestID(ctx), "panic", r, "stack", string(debug.Stack()))
		*err = status.Error(codes.Internal, internalMessage)
	}
}

// contextStream is a server stream whose context carries the request ID.
type contextStream struct {
	grpc.ServerStream
	ctx context.Context
}

// Context is the stream's context with the request ID.
func (s *contextStream) Context() context.Context { return s.ctx }

func requestIDFromIncoming(ctx context.Context) context.Context {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if ids := md.Get(RequestIDHeader); len(ids) > 0 && ids[0] != "" {
			return WithRequestID(ctx, ids[0])
		}
	}
	return ctx
}
