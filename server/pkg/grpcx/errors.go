// Package grpcx is the shared gRPC plumbing: server, client, request IDs and error reasons.
// Errors cross calls as a status with an ErrorInfo reason; their text never leaves the service.
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

// Key is the Reason for enum value e, as ReasonOf reports it.
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

const internalMessage = "internal error"

// Status is err as it leaves the service, and whether it was explained (else log it). A downstream
// status keeps its code and its reason, if any, never its text: a dependency's Unavailable or
// DeadlineExceeded tells the caller to retry, and its rules travel as reasons. An unknown error is Internal.
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
		if r, has := reasonIn(st); has {
			return withReason(st.Code(), r), true
		}
		return status.New(st.Code(), st.Code().String()), st.Code() == codes.Canceled
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

// IsRule reports whether a failed call carries a reason, so a retry cannot help.
func IsRule(err error) bool {
	_, ok := ReasonOf(err)
	return ok
}

// ErrorFor is a failure carrying r, as a table row would send it.
func ErrorFor(code codes.Code, r Reason) error {
	return withReason(code, r).Err()
}
