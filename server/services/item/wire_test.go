package item_test

import (
	"errors"
	"log/slog"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/reflect/protoreflect"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/pkg/observability/metrics"
	"finga/pkg/transient"
	"finga/services/item"
	"finga/services/item/internal/handler"
	"finga/services/item/internal/model"
)

// Each error a call meets leaves the service with the code and reason the contract gave it before the module had a facade.
// The rows are literals, so a row changed in either the facade's table or the handler's fails here.
func TestEachErrorKeepsItsWire(t *testing.T) {
	for _, tt := range []struct {
		err    error
		code   codes.Code
		reason protoreflect.Enum // nil for none
	}{
		{model.ErrNotFound, codes.NotFound, itemv1.Reason_REASON_ITEM_NOT_FOUND},
		{model.ErrSlotTaken, codes.FailedPrecondition, itemv1.Reason_REASON_SLOT_TAKEN},
		{model.ErrDifferentItem, codes.FailedPrecondition, itemv1.Reason_REASON_SLOT_HOLDS_OTHER_ITEM},
		{model.ErrStackFull, codes.FailedPrecondition, itemv1.Reason_REASON_STACK_FULL},
		{model.ErrEquipTabFull, codes.FailedPrecondition, itemv1.Reason_REASON_EQUIP_TAB_FULL},
		{model.ErrUseTabFull, codes.FailedPrecondition, itemv1.Reason_REASON_USE_TAB_FULL},
		{model.ErrEtcTabFull, codes.FailedPrecondition, itemv1.Reason_REASON_ETC_TAB_FULL},
		{model.ErrKeyReused, codes.FailedPrecondition, itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED},
		{model.ErrBadKey, codes.InvalidArgument, itemv1.Reason_REASON_BAD_IDEMPOTENCY_KEY},
		{model.ErrBadCause, codes.InvalidArgument, itemv1.Reason_REASON_BAD_CAUSE},
		{model.ErrBadQuantity, codes.InvalidArgument, itemv1.Reason_REASON_BAD_QUANTITY},
		{model.ErrBadSlot, codes.InvalidArgument, itemv1.Reason_REASON_BAD_SLOT},
		{model.ErrWrongTab, codes.InvalidArgument, itemv1.Reason_REASON_WRONG_TAB},
		{model.ErrUnknownItem, codes.InvalidArgument, itemv1.Reason_REASON_UNKNOWN_ITEM},
		{model.ErrBadRoll, codes.InvalidArgument, itemv1.Reason_REASON_BAD_ROLL},
		{model.ErrItemWorn, codes.FailedPrecondition, itemv1.Reason_REASON_ITEM_WORN},
		{model.ErrLevelTooLow, codes.FailedPrecondition, itemv1.Reason_REASON_LEVEL_TOO_LOW},
		{model.ErrWrongClass, codes.FailedPrecondition, itemv1.Reason_REASON_WRONG_CLASS},
		{model.ErrSTRTooLow, codes.FailedPrecondition, itemv1.Reason_REASON_STR_TOO_LOW},
		{model.ErrDEXTooLow, codes.FailedPrecondition, itemv1.Reason_REASON_DEX_TOO_LOW},
		{model.ErrINTTooLow, codes.FailedPrecondition, itemv1.Reason_REASON_INT_TOO_LOW},
		{model.ErrAGITooLow, codes.FailedPrecondition, itemv1.Reason_REASON_AGI_TOO_LOW},
		{model.ErrVITTooLow, codes.FailedPrecondition, itemv1.Reason_REASON_VIT_TOO_LOW},
		{model.ErrWrongEquipSlot, codes.InvalidArgument, itemv1.Reason_REASON_WRONG_EQUIP_SLOT},
		{model.ErrNotEquipment, codes.FailedPrecondition, itemv1.Reason_REASON_NOT_EQUIPMENT},
		{model.ErrNothingWorn, codes.FailedPrecondition, itemv1.Reason_REASON_NOTHING_WORN},
		{model.ErrBadBatch, codes.InvalidArgument, itemv1.Reason_REASON_BAD_BATCH_SIZE},
		{model.ErrNotEnoughItems, codes.FailedPrecondition, itemv1.Reason_REASON_NOT_ENOUGH_ITEMS},
		{model.ErrNotUsable, codes.FailedPrecondition, itemv1.Reason_REASON_NOT_USABLE},
		{kernel.ErrBadID, codes.InvalidArgument, itemv1.Reason_REASON_BAD_ID},
		{kernel.ErrUnknownClass, codes.InvalidArgument, itemv1.Reason_REASON_UNKNOWN_CLASS},
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
	h, err := host.New(host.Options{Name: "item", Logger: slog.New(slog.DiscardHandler), Registry: metrics.NewRegistry()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	mod, err := h.Module("item")
	if err != nil {
		t.Fatal(err)
	}
	calls, err := mod.Calls(item.ErrorTable)
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
