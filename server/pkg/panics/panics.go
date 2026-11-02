// Package panics turns recovered panics into errors, and logs and counts them the same way in every flow.
package panics

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
)

// The flows a panic is counted under.
const (
	FlowHTTP   = "http"   // a REST request
	FlowGRPC   = "grpc"   // a gRPC call or stream
	FlowWS     = "ws"     // a WebSocket message or connection
	FlowStream = "stream" // a message from a followed stream
)

// Error is a recovered panic and the stack it happened on.
type Error struct {
	Value any
	Stack []byte
}

// Of describes a value just recovered; call it while the panicking frames are still on the stack.
func Of(value any) *Error { return &Error{Value: value, Stack: debug.Stack()} }

func (e *Error) Error() string { return fmt.Sprintf("panic: %v", e.Value) }

// Capture turns a panic into an *Error in *err; deferred, it lets a goroutine return its panic.
func Capture(err *error) {
	if p := recover(); p != nil {
		*err = Of(p)
	}
}

// Counter counts recovered panics by flow; observability.PanicCounter is the Prometheus one.
type Counter interface{ Inc(flow string) }

// Recorder logs and counts recovered panics; a process has one, shared by every flow.
type Recorder struct {
	log   *slog.Logger
	count Counter
}

// NewRecorder logs to log and counts in count; a nil count only logs.
func NewRecorder(log *slog.Logger, count Counter) *Recorder { return &Recorder{log: log, count: count} }

// Record logs e with its stack and attrs, and counts it under flow.
func (r *Recorder) Record(ctx context.Context, flow string, e *Error, attrs ...any) {
	if r.count != nil {
		r.count.Inc(flow)
	}
	args := append([]any{"flow", flow, "panic", fmt.Sprint(e.Value), "stack", string(e.Stack)}, attrs...)
	r.log.ErrorContext(ctx, "panic recovered", args...)
}
