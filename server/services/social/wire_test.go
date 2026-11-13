package social_test

import (
	"errors"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/reflect/protoreflect"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/observability/metrics"
	"finga/pkg/transient"
	"finga/services/social"
	"finga/services/social/internal/handler"
	"finga/services/social/internal/model"
)

// Each error a call meets leaves the service with the code and reason the contract gave it before the module had a facade.
// The rows are literals, so a row changed in either the facade's table or the handler's fails here.
func TestEachErrorKeepsItsWire(t *testing.T) {
	for _, tt := range []struct {
		err    error
		code   codes.Code
		reason protoreflect.Enum // nil for none
		retry  bool              // a retry keyed on transient.ErrUnavailable retries it
	}{
		{model.ErrNotLeader, codes.FailedPrecondition, socialv1.Reason_REASON_NOT_PARTY_LEADER, false},
		{model.ErrFull, codes.FailedPrecondition, socialv1.Reason_REASON_PARTY_FULL, false},
		{model.ErrInParty, codes.FailedPrecondition, socialv1.Reason_REASON_ALREADY_IN_PARTY, false},
		{model.ErrTargetInParty, codes.FailedPrecondition, socialv1.Reason_REASON_TARGET_IN_PARTY, false},
		{model.ErrNoParty, codes.NotFound, socialv1.Reason_REASON_NO_PARTY, false},
		{model.ErrNotMember, codes.NotFound, socialv1.Reason_REASON_NOT_PARTY_MEMBER, false},
		{model.ErrNotHere, codes.NotFound, socialv1.Reason_REASON_TARGET_NOT_ON_MAP, false},
		{model.ErrAlreadyAsked, codes.AlreadyExists, socialv1.Reason_REASON_ALREADY_INVITED, false},
		{model.ErrInviteExpired, codes.NotFound, socialv1.Reason_REASON_INVITE_EXPIRED, false},
		{model.ErrSelf, codes.InvalidArgument, socialv1.Reason_REASON_INVITE_SELF, false},
		{model.ErrEmpty, codes.InvalidArgument, socialv1.Reason_REASON_CHAT_EMPTY, false},
		{model.ErrTooLong, codes.InvalidArgument, socialv1.Reason_REASON_CHAT_TOO_LONG, false},
		{model.ErrChannel, codes.InvalidArgument, socialv1.Reason_REASON_BAD_CHAT_CHANNEL, false},
		{model.ErrChatNoParty, codes.NotFound, socialv1.Reason_REASON_CHAT_NO_PARTY, false},
		{model.ErrRate, codes.ResourceExhausted, socialv1.Reason_REASON_CHAT_RATE_LIMITED, false},
		{kernel.ErrBadID, codes.InvalidArgument, socialv1.Reason_REASON_BAD_ID, false},
		{transient.ErrUnavailable, codes.Unavailable, nil, true},
		{errors.New("disk on fire"), codes.Internal, nil, false},
	} {
		t.Run(tt.err.Error(), func(t *testing.T) {
			code, reason, has, err := wire(t, func() error { return tt.err })
			if code != tt.code || has != (tt.reason != nil) || has && reason != grpcx.Key(tt.reason) {
				t.Fatalf("left as %s %v, want %s %v", code, reason, tt.code, tt.reason)
			}
			if retry := errors.Is(err, transient.ErrUnavailable); retry != tt.retry {
				t.Fatalf("a caller retrying on transient.ErrUnavailable retries it: %v, want %v", retry, tt.retry)
			}
		})
	}
}

// A panic in a call answers as one the gRPC server recovers did: Internal, with the usual text and no reason.
func TestAPanicKeepsItsWire(t *testing.T) {
	code, _, has, _ := wire(t, func() error { panic("till jammed") })
	if code != codes.Internal || has {
		t.Fatalf("left as %s, reason %v", code, has)
	}
}

// wire is the code and reason an error met inside a call leaves the service with, through the module's facade, then the handler's table, and the error the facade returned.
func wire(t *testing.T, do func() error) (codes.Code, grpcx.Reason, bool, error) {
	t.Helper()
	h, err := host.New(host.Options{Name: "social", Logger: slog.New(slog.DiscardHandler), Registry: metrics.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	mod, err := h.Module("social")
	if err != nil {
		t.Fatal(err)
	}
	calls, err := mod.Calls(social.ErrorTable)
	if err != nil {
		t.Fatal(err)
	}
	err = func() (err error) {
		_, call := calls.Start(t.Context(), "Wire")
		defer call.End(&err)
		return do()
	}()
	st, _ := handler.Errors.Status(err)
	if st.Code() == codes.Internal && st.Message() != "internal error" {
		t.Fatalf("Internal says %q", st.Message())
	}
	reason, has := grpcx.ReasonOf(st.Err())
	return st.Code(), reason, has, err
}
