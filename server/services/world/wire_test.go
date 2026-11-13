package world_test

import (
	"errors"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/reflect/protoreflect"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/observability/metrics"
	"finga/pkg/transient"
	"finga/services/world"
	"finga/services/world/internal/handler"
	"finga/services/world/internal/model"
)

// Each error a call meets leaves the service with the code and reason the contract gave it before the module had a facade.
// The rows are literals, so a row changed in either the facade's table or the handler's fails here.
func TestEachErrorKeepsItsWire(t *testing.T) {
	for _, tt := range []struct {
		err    error
		code   codes.Code
		reason protoreflect.Enum // nil for none
	}{
		{model.ErrBusy, codes.Unavailable, worldv1.Reason_REASON_BUSY},
		{model.ErrNoMap, codes.NotFound, worldv1.Reason_REASON_NO_MAP},
		{model.ErrStopped, codes.Unavailable, worldv1.Reason_REASON_STOPPED},
		{model.ErrClassUnavailable, codes.FailedPrecondition, worldv1.Reason_REASON_CLASS_UNAVAILABLE},
		{model.ErrCharacterDead, codes.FailedPrecondition, worldv1.Reason_REASON_CHARACTER_DEAD},
		{model.ErrPointsChanged, codes.FailedPrecondition, worldv1.Reason_REASON_POINTS_CHANGED},
		{model.ErrNotEnoughPoints, codes.FailedPrecondition, worldv1.Reason_REASON_NOT_ENOUGH_POINTS},
		{model.ErrNoFreeReset, codes.FailedPrecondition, worldv1.Reason_REASON_NO_FREE_RESET},
		{model.ErrBadPoints, codes.InvalidArgument, worldv1.Reason_REASON_BAD_POINTS},
		{model.ErrSkillPointsChanged, codes.FailedPrecondition, worldv1.Reason_REASON_SKILL_POINTS_CHANGED},
		{model.ErrNotEnoughSkillPoints, codes.FailedPrecondition, worldv1.Reason_REASON_NOT_ENOUGH_SKILL_POINTS},
		{model.ErrSkillNotLearnable, codes.FailedPrecondition, worldv1.Reason_REASON_SKILL_NOT_LEARNABLE},
		{model.ErrBadSkillPoints, codes.InvalidArgument, worldv1.Reason_REASON_BAD_SKILL_POINTS},
		{model.ErrSkillNotLearned, codes.FailedPrecondition, worldv1.Reason_REASON_SKILL_NOT_LEARNED},
		{model.ErrSkillPassive, codes.FailedPrecondition, worldv1.Reason_REASON_SKILL_PASSIVE},
		{model.ErrNotInTown, codes.FailedPrecondition, worldv1.Reason_REASON_NOT_IN_TOWN},
		{model.ErrSkillCoolingDown, codes.FailedPrecondition, worldv1.Reason_REASON_SKILL_COOLING_DOWN},
		{model.ErrNotEnoughMP, codes.FailedPrecondition, worldv1.Reason_REASON_NOT_ENOUGH_MP},
		{model.ErrSkillWaiting, codes.FailedPrecondition, worldv1.Reason_REASON_SKILL_WAITING},
		{model.ErrPromotionUnmet, codes.FailedPrecondition, worldv1.Reason_REASON_PROMOTION_CONDITIONS_UNMET},
		{model.ErrNotYourNextClass, codes.FailedPrecondition, worldv1.Reason_REASON_NOT_YOUR_NEXT_CLASS},
		{model.ErrNpcOutOfReach, codes.FailedPrecondition, worldv1.Reason_REASON_NPC_OUT_OF_REACH},
		{model.ErrQuestUnavailable, codes.FailedPrecondition, worldv1.Reason_REASON_QUEST_UNAVAILABLE},
		{model.ErrQuestUnmet, codes.FailedPrecondition, worldv1.Reason_REASON_QUEST_CONDITIONS_UNMET},
		{model.ErrQuestActive, codes.FailedPrecondition, worldv1.Reason_REASON_QUEST_ALREADY_ACTIVE},
		{model.ErrQuestFinished, codes.FailedPrecondition, worldv1.Reason_REASON_QUEST_ALREADY_FINISHED},
		{model.ErrTooManyQuests, codes.FailedPrecondition, worldv1.Reason_REASON_TOO_MANY_QUESTS},
		{model.ErrQuestNotReady, codes.FailedPrecondition, worldv1.Reason_REASON_QUEST_NOT_READY},
		{model.ErrHPFull, codes.FailedPrecondition, worldv1.Reason_REASON_HP_FULL},
		{model.ErrMPFull, codes.FailedPrecondition, worldv1.Reason_REASON_MP_FULL},
		{model.ErrNotUsable, codes.FailedPrecondition, worldv1.Reason_REASON_NOT_USABLE},
		{kernel.ErrNotSubscribed, codes.FailedPrecondition, worldv1.Reason_REASON_NOT_SUBSCRIBED}, // the handler's own error before the facade
		{kernel.ErrBadID, codes.InvalidArgument, worldv1.Reason_REASON_BAD_ID},
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
	h, err := host.New(host.Options{Name: "world", Logger: slog.New(slog.DiscardHandler), Registry: metrics.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	mod, err := h.Module("world")
	if err != nil {
		t.Fatal(err)
	}
	calls, err := mod.Calls(world.ErrorTable)
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
