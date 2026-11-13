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
	}{
		{model.ErrNotLeader, codes.FailedPrecondition, socialv1.Reason_REASON_NOT_PARTY_LEADER},
		{model.ErrFull, codes.FailedPrecondition, socialv1.Reason_REASON_PARTY_FULL},
		{model.ErrInParty, codes.FailedPrecondition, socialv1.Reason_REASON_ALREADY_IN_PARTY},
		{model.ErrTargetInParty, codes.FailedPrecondition, socialv1.Reason_REASON_TARGET_IN_PARTY},
		{model.ErrNoParty, codes.NotFound, socialv1.Reason_REASON_NO_PARTY},
		{model.ErrNotMember, codes.NotFound, socialv1.Reason_REASON_NOT_PARTY_MEMBER},
		{model.ErrNotHere, codes.NotFound, socialv1.Reason_REASON_TARGET_NOT_ON_MAP},
		{model.ErrAlreadyAsked, codes.AlreadyExists, socialv1.Reason_REASON_ALREADY_INVITED},
		{model.ErrInviteExpired, codes.NotFound, socialv1.Reason_REASON_INVITE_EXPIRED},
		{model.ErrSelf, codes.InvalidArgument, socialv1.Reason_REASON_INVITE_SELF},
		{model.ErrEmpty, codes.InvalidArgument, socialv1.Reason_REASON_CHAT_EMPTY},
		{model.ErrTooLong, codes.InvalidArgument, socialv1.Reason_REASON_CHAT_TOO_LONG},
		{model.ErrChannel, codes.InvalidArgument, socialv1.Reason_REASON_BAD_CHAT_CHANNEL},
		{model.ErrChatNoParty, codes.NotFound, socialv1.Reason_REASON_CHAT_NO_PARTY},
		{model.ErrRate, codes.ResourceExhausted, socialv1.Reason_REASON_CHAT_RATE_LIMITED},
		{kernel.ErrBadID, codes.InvalidArgument, socialv1.Reason_REASON_BAD_ID},
		{transient.ErrUnavailable, codes.Unavailable, nil},
		{errors.New("disk on fire"), codes.Internal, nil},
	} {
		t.Run(tt.err.Error(), func(t *testing.T) {
			code, reason, has := wire(t, func() error { return tt.err })
			if code != tt.code || has != (tt.reason != nil) || has && reason != grpcx.Key(tt.reason) {
				t.Fatalf("left as %s %v, want %s %v", code, reason, tt.code, tt.reason)
			}
		})
	}
}

// A panic in a call answers as one the gRPC server recovers did: Internal, with the usual text and no reason.
func TestAPanicKeepsItsWire(t *testing.T) {
	code, _, has := wire(t, func() error { panic("till jammed") })
	if code != codes.Internal || has {
		t.Fatalf("left as %s, reason %v", code, has)
	}
}

// wire is the code and reason an error met inside a call leaves the service with: through the module's facade, then the handler's table.
func wire(t *testing.T, do func() error) (codes.Code, grpcx.Reason, bool) {
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
	return st.Code(), reason, has
}
