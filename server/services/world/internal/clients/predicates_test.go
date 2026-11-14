package clients_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	itemv1 "finga/api/gen/item/v1"
	socialv1 "finga/api/gen/social/v1"
	worldv1 "finga/api/gen/world/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx/grpctest"
	"finga/pkg/transient"
	"finga/services/world/internal/clients"
	"finga/services/world/internal/local"
)

// predicates are the world's retry and refusal checks on its calls to other modules, in process and over gRPC.
var predicates = []struct {
	name        string
	local, wire func(error) bool
}{
	{"Loot.Retryable", local.Loot{}.Retryable, clients.Loot{}.Retryable},
	{"Saver.Retryable", local.Saver{}.Retryable, clients.Saver{}.Retryable},
	{"Saver.Refused", local.Saver{}.Refused, clients.Saver{}.Refused},
	{"Social.Retryable", local.Social{}.Retryable, clients.Social{}.Retryable},
}

// Each failure a module answers in process, beside the status it reached the world as over gRPC, answers each check alike; the two that don't, no call the world makes meets.
func TestTheRetryPredicatesCarryOverUnchanged(t *testing.T) {
	cause := errors.New("the module's cause")
	failures := []struct {
		name        string
		local, wire error
		want        [4]bool         // each predicate's answer, in order
		unreachable map[string]bool // predicates whose wire answer differs, for a failure no such call meets
	}{
		{"a module's own failure", kernel.ErrInternal, status.Error(codes.Internal, "internal error"), [4]bool{true, false, false, false}, nil},
		{"a module down or busy for now", fmt.Errorf("%w: %w", transient.ErrUnavailable, cause), status.Error(codes.Unavailable, "Unavailable"), [4]bool{true, true, false, true}, nil},
		{"a final rule", fmt.Errorf("%w: %w", kernel.ErrEquipTabFull, cause), grpctest.Error(codes.FailedPrecondition, itemv1.Reason_REASON_EQUIP_TAB_FULL), [4]bool{false, false, true, false}, nil},
		{"a malformed ID", kernel.ErrBadID, grpctest.Error(codes.InvalidArgument, itemv1.Reason_REASON_BAD_ID), [4]bool{false, false, true, false}, nil},
		{"out of time", context.DeadlineExceeded, status.Error(codes.DeadlineExceeded, "DeadlineExceeded"), [4]bool{true, true, false, true}, nil},
		{"given up", context.Canceled, status.Error(codes.Canceled, "Canceled"), [4]bool{true, true, false, true}, nil},
		// A retryable rule: item and character declare none, so no grant or save meets one.
		{"a rule a retry may pass", fmt.Errorf("%w: %w", kernel.ErrMapBusy, cause), grpctest.Error(codes.Unavailable, worldv1.Reason_REASON_BUSY), [4]bool{true, true, false, true},
			map[string]bool{"Loot.Retryable": true, "Saver.Refused": true}},
		// Aborted and ResourceExhausted come only from a login, a session race or a chat line, none of which the world calls.
		{"a rate limit", fmt.Errorf("%w: %w", kernel.ErrChatRateLimited, cause), grpctest.Error(codes.ResourceExhausted, socialv1.Reason_REASON_CHAT_RATE_LIMITED), [4]bool{false, false, true, false},
			map[string]bool{"Saver.Retryable": true, "Social.Retryable": true}},
	}
	for _, f := range failures {
		for k, p := range predicates {
			if got := p.local(f.local); got != f.want[k] {
				t.Errorf("%s: in process, %s answers %v", f.name, p.name, got)
			}
			if got := p.wire(f.wire); got != f.want[k] && !f.unreachable[p.name] {
				t.Errorf("%s: over gRPC, %s answered %v", f.name, p.name, got)
			} else if got == f.want[k] && f.unreachable[p.name] {
				t.Errorf("%s: over gRPC, %s answered as in process; it is no difference to list", f.name, p.name)
			}
		}
	}
}
