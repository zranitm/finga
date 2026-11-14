package clients_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	accountv1 "finga/api/gen/account/v1"
	itemv1 "finga/api/gen/item/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/transient"
	"finga/services/character/internal/clients"
	"finga/services/character/internal/local"
)

// Each failure the item module answers a starter grant in process, beside the status it reached character as over gRPC, is retried alike; a retryable rule, which item declares none of, differs.
func TestTheStarterGrantRetriesAsOverGRPC(t *testing.T) {
	cause := errors.New("the module's cause")
	for _, f := range []struct {
		name        string
		local, wire error
		retry       bool
		unreachable bool // the wire answered otherwise, for a failure item never sends
	}{
		{"a module's own failure", kernel.ErrInternal, status.Error(codes.Internal, "internal error"), true, false},
		{"a module down or busy for now", fmt.Errorf("%w: %w", transient.ErrUnavailable, cause), status.Error(codes.Unavailable, "Unavailable"), true, false},
		{"a final rule", fmt.Errorf("%w: %w", kernel.ErrUnknownItem, cause), grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_UNKNOWN_ITEM), false, false},
		{"a malformed ID", kernel.ErrBadID, grpctest.Error(codes.InvalidArgument, itemv1.Reason_REASON_BAD_ID), false, false},
		{"out of time", context.DeadlineExceeded, status.Error(codes.DeadlineExceeded, "DeadlineExceeded"), true, false},
		{"given up", context.Canceled, status.Error(codes.Canceled, "Canceled"), true, false},
		{"a session race", fmt.Errorf("%w: %w", kernel.ErrSessionRace, cause), grpctest.Error(codes.Aborted, accountv1.Reason_REASON_SESSION_RACE), false, false},
		{"a rule a retry may pass", fmt.Errorf("%w: %w", kernel.ErrMapBusy, cause), grpctest.Error(codes.Unavailable, worldv1.Reason_REASON_BUSY), true, true},
	} {
		if got := (local.Starter{}).Retryable(f.local); got != f.retry {
			t.Errorf("%s: in process, retried %v", f.name, got)
		}
		if got := (clients.Starter{}).Retryable(f.wire); (got == f.retry) == f.unreachable {
			t.Errorf("%s: over gRPC, retried %v", f.name, got)
		}
	}
}
