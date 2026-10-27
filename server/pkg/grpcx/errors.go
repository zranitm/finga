// Package grpcx is the gRPC plumbing every service shares: a server whose
// errors keep a reason across the call and never their text, a client
// connection, the request ID in metadata, and a loop that keeps a server
// stream open. It knows nothing of any service: a service brings its own
// error table and proto enum of reasons.
//
// A failure crosses a call as a status with a google.rpc.ErrorInfo detail:
// its Reason is the name of a value of the service's reason enum, and its
// Domain is that enum's proto package. The caller reads it back with
// ReasonOf; nothing else of the original error, not even its message,
// leaves the service.
package grpcx

import (
	"context"
	"errors"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Reason names a failure across a call.
type Reason struct {
	Domain string // the proto package of the reason enum
	Name   string // the enum value's name
}

// String is domain/name.
func (r Reason) String() string { return r.Domain + "/" + r.Name }

// Key is the Reason a table row with this enum value produces, and what
// ReasonOf then reports.
func Key(e protoreflect.Enum) Reason {
	d := e.Descriptor()
	name := ""
	if v := d.Values().ByNumber(e.Number()); v != nil {
		name = string(v.Name())
	}
	return Reason{Domain: string(d.ParentFile().Package()), Name: name}
}

// ErrorRow maps one sentinel error to a gRPC code and a reason.
type ErrorRow struct {
	Err    error
	Code   codes.Code
	Reason protoreflect.Enum
}

// ErrorTable is a service's errors, each with its code and reason.
type ErrorTable []ErrorRow

// internalMessage is the only text an unexpected failure carries.
const internalMessage = "internal error"

// Status is err as it leaves the service, and whether the table (or a
// reason from a downstream call) explained it; an unexplained error is the
// handler's to log with its cause.
//   - A sentinel in the table, also wrapped: its code, with ErrorInfo.
//   - An error that already carries a reason (a downstream call's, passed
//     on): its code and details, unchanged.
//   - A cancelled or expired context: Canceled or DeadlineExceeded.
//   - A downstream status without a reason: Unavailable and
//     DeadlineExceeded keep their code (a retry may help); anything else
//     is Internal. The text is dropped either way.
//   - Anything else: Internal.
func (t ErrorTable) Status(err error) (*status.Status, bool) {
	if err == nil {
		return status.New(codes.OK, ""), true
	}
	for _, row := range t {
		if errors.Is(err, row.Err) {
			return withReason(row.Code, Key(row.Reason)), true
		}
	}
	if st, ok := status.FromError(err); ok {
		if _, has := reasonIn(st); has {
			return st, true
		}
		switch st.Code() {
		case codes.Unavailable, codes.DeadlineExceeded, codes.Canceled:
			return status.New(st.Code(), st.Code().String()), false
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return status.New(codes.Canceled, codes.Canceled.String()), true
	case errors.Is(err, context.DeadlineExceeded):
		return status.New(codes.DeadlineExceeded, codes.DeadlineExceeded.String()), true
	}
	return status.New(codes.Internal, internalMessage), false
}

func withReason(code codes.Code, r Reason) *status.Status {
	st, err := status.New(code, r.Name).WithDetails(&errdetails.ErrorInfo{Reason: r.Name, Domain: r.Domain})
	if err != nil { // only for a detail that cannot be marshalled
		return status.New(code, r.Name)
	}
	return st
}

// ReasonOf reads the reason a failed call carries, also through %w.
func ReasonOf(err error) (Reason, bool) {
	if err == nil {
		return Reason{}, false
	}
	st, ok := status.FromError(err)
	if !ok {
		return Reason{}, false
	}
	return reasonIn(st)
}

func reasonIn(st *status.Status) (Reason, bool) {
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok && info.GetReason() != "" {
			return Reason{Domain: info.GetDomain(), Name: info.GetReason()}, true
		}
	}
	return Reason{}, false
}

// IsRule reports whether a failed call ran into a rule of the service
// (it carries a reason), so retrying the same request cannot help. Any
// other failure (unreachable, timed out, internal) may pass on a retry.
func IsRule(err error) bool {
	_, ok := ReasonOf(err)
	return ok
}
