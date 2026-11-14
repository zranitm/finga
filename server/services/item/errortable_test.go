package item_test

import (
	"errors"
	"log/slog"
	"testing"

	"finga/internal/host"
	"finga/internal/kernel"
	"finga/pkg/observability/metrics"
	"finga/pkg/transient"
	"finga/services/item"
	"finga/services/item/internal/model"
)

// Each error a call meets reaches callers as its kernel error, with its reason.
// The rows are literals, so a row changed in the facade's table or a reason changed in the kernel fails here.
func TestEachErrorReachesCallersAsItsKernelError(t *testing.T) {
	for _, tt := range []struct {
		err    error
		kernel error  // nil for none
		reason string // "" for none
		retry  bool   // a retry keyed on transient.ErrUnavailable retries it
	}{
		{model.ErrNotFound, kernel.ErrItemNotFound, "item_not_found", false},
		{model.ErrSlotTaken, kernel.ErrSlotTaken, "slot_taken", false},
		{model.ErrDifferentItem, kernel.ErrSlotHoldsOtherItem, "slot_holds_other_item", false},
		{model.ErrStackFull, kernel.ErrStackFull, "stack_full", false},
		{model.ErrEquipTabFull, kernel.ErrEquipTabFull, "equip_tab_full", false},
		{model.ErrUseTabFull, kernel.ErrUseTabFull, "use_tab_full", false},
		{model.ErrEtcTabFull, kernel.ErrEtcTabFull, "etc_tab_full", false},
		{model.ErrKeyReused, kernel.ErrIdempotencyKeyReused, "idempotency_key_reused", false},
		{model.ErrBadKey, kernel.ErrBadIdempotencyKey, "bad_idempotency_key", false},
		{model.ErrBadCause, kernel.ErrBadCause, "bad_cause", false},
		{model.ErrBadQuantity, kernel.ErrBadQuantity, "bad_quantity", false},
		{model.ErrBadSlot, kernel.ErrBadSlot, "bad_slot", false},
		{model.ErrWrongTab, kernel.ErrWrongTab, "wrong_tab", false},
		{model.ErrUnknownItem, kernel.ErrUnknownItem, "unknown_item", false},
		{model.ErrBadRoll, kernel.ErrBadRoll, "bad_roll", false},
		{model.ErrItemWorn, kernel.ErrItemWorn, "item_worn", false},
		{model.ErrLevelTooLow, kernel.ErrLevelTooLow, "level_too_low", false},
		{model.ErrWrongClass, kernel.ErrWrongClass, "wrong_class", false},
		{model.ErrSTRTooLow, kernel.ErrSTRTooLow, "str_too_low", false},
		{model.ErrDEXTooLow, kernel.ErrDEXTooLow, "dex_too_low", false},
		{model.ErrINTTooLow, kernel.ErrINTTooLow, "int_too_low", false},
		{model.ErrAGITooLow, kernel.ErrAGITooLow, "agi_too_low", false},
		{model.ErrVITTooLow, kernel.ErrVITTooLow, "vit_too_low", false},
		{model.ErrWrongEquipSlot, kernel.ErrWrongEquipSlot, "wrong_equip_slot", false},
		{model.ErrNotEquipment, kernel.ErrNotEquipment, "not_equipment", false},
		{model.ErrNothingWorn, kernel.ErrNothingWorn, "nothing_worn", false},
		{model.ErrBadBatch, kernel.ErrBadBatchSize, "bad_batch_size", false},
		{model.ErrNotEnoughItems, kernel.ErrNotEnoughItems, "not_enough_items", false},
		{model.ErrNotUsable, kernel.ErrNotUsable, "not_usable", false},
		{kernel.ErrBadID, kernel.ErrBadID, "bad_id", false},
		{kernel.ErrUnknownClass, kernel.ErrUnknownClass, "unknown_class", false},
		{transient.ErrUnavailable, nil, "", true},
		{errors.New("disk on fire"), nil, "", false}, // passes as it is, so a caller reads it as internal
	} {
		t.Run(tt.err.Error(), func(t *testing.T) {
			err := call(t, func() error { return tt.err })
			if tt.kernel != nil && !errors.Is(err, tt.kernel) || !errors.Is(err, tt.err) {
				t.Fatalf("reached callers as %v; want %v wrapping %v", err, tt.kernel, tt.err)
			}
			if reason, ok := kernel.ReasonOf(err); reason != tt.reason || ok != (tt.reason != "") {
				t.Fatalf("reason %q (%v), want %q", reason, ok, tt.reason)
			}
			if retry := errors.Is(err, transient.ErrUnavailable); retry != tt.retry {
				t.Fatalf("a caller retrying on transient.ErrUnavailable retries it: %v, want %v", retry, tt.retry)
			}
		})
	}
}

// A panic in a call reaches callers as kernel.ErrInternal, with no reason.
func TestAPanicIsInternal(t *testing.T) {
	err := call(t, func() error { panic("till jammed") })
	if _, has := kernel.ReasonOf(err); !errors.Is(err, kernel.ErrInternal) || has {
		t.Fatalf("reached callers as %v", err)
	}
}

// call is the error a call that met do's returns, through the module's facade as a caller in the process gets it.
func call(t *testing.T, do func() error) (err error) {
	t.Helper()
	h, hostErr := host.New(host.Options{Name: "finga", Logger: slog.New(slog.DiscardHandler), Registry: metrics.NewRegistry()})
	if hostErr != nil {
		t.Fatal(hostErr)
	}
	t.Cleanup(h.Close)
	mod, hostErr := h.Module("item")
	if hostErr != nil {
		t.Fatal(hostErr)
	}
	calls, hostErr := mod.Calls(item.ErrorTable)
	if hostErr != nil {
		t.Fatal(hostErr)
	}
	_, c := calls.Start(t.Context(), "Call")
	defer c.End(&err)
	return do()
}
