package character_test

import (
	"errors"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/reflect/protoreflect"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/observability/metrics"
	"finga/pkg/transient"
	"finga/services/character"
	"finga/services/character/internal/handler"
	"finga/services/character/internal/model"
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
		{model.ErrNotFound, codes.NotFound, characterv1.Reason_REASON_CHARACTER_NOT_FOUND, false},
		{model.ErrNameTaken, codes.AlreadyExists, characterv1.Reason_REASON_NAME_TAKEN, false},
		{model.ErrBadName, codes.InvalidArgument, characterv1.Reason_REASON_BAD_CHARACTER_NAME, false},
		{model.ErrClassUnavailable, codes.InvalidArgument, characterv1.Reason_REASON_CLASS_UNAVAILABLE, false},
		{model.ErrSlotsFull, codes.FailedPrecondition, characterv1.Reason_REASON_CHARACTER_SLOTS_FULL, false},
		{model.ErrBadQuest, codes.InvalidArgument, characterv1.Reason_REASON_BAD_QUEST, false},
		{model.ErrBadSkill, codes.InvalidArgument, characterv1.Reason_REASON_BAD_SKILL, false},
		{model.ErrBadSkillTimer, codes.InvalidArgument, characterv1.Reason_REASON_BAD_SKILL_TIMER, false},
		{model.ErrBadCheckpoint, codes.InvalidArgument, characterv1.Reason_REASON_BAD_CHECKPOINT, false},
		{model.ErrBadSkillBar, codes.InvalidArgument, characterv1.Reason_REASON_BAD_SKILL_BAR, false},
		{kernel.ErrBadID, codes.InvalidArgument, characterv1.Reason_REASON_BAD_ID, false},
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
	h, err := host.New(host.Options{Name: "character", Logger: slog.New(slog.DiscardHandler), Registry: metrics.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	mod, err := h.Module("character")
	if err != nil {
		t.Fatal(err)
	}
	calls, err := mod.Calls(character.ErrorTable)
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
