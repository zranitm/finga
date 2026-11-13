package handler

import (
	"google.golang.org/grpc/codes"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
)

// Errors maps every kernel error the module returns, and a malformed request, to its gRPC code and contract reason.
var Errors = grpcx.ErrorTable{
	{Err: kernel.ErrItemNotFound, Code: codes.NotFound, Reason: itemv1.Reason_REASON_ITEM_NOT_FOUND},
	{Err: kernel.ErrSlotTaken, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_SLOT_TAKEN},
	{Err: kernel.ErrSlotHoldsOtherItem, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_SLOT_HOLDS_OTHER_ITEM},
	{Err: kernel.ErrStackFull, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_STACK_FULL},
	{Err: kernel.ErrEquipTabFull, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_EQUIP_TAB_FULL},
	{Err: kernel.ErrUseTabFull, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_USE_TAB_FULL},
	{Err: kernel.ErrEtcTabFull, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_ETC_TAB_FULL},
	{Err: kernel.ErrIdempotencyKeyReused, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED},
	{Err: kernel.ErrBadIdempotencyKey, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_IDEMPOTENCY_KEY},
	{Err: kernel.ErrBadCause, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_CAUSE},
	{Err: kernel.ErrBadQuantity, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_QUANTITY},
	{Err: kernel.ErrBadSlot, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_SLOT},
	{Err: kernel.ErrWrongTab, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_WRONG_TAB},
	{Err: kernel.ErrUnknownItem, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_UNKNOWN_ITEM},
	{Err: kernel.ErrBadRoll, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_ROLL},
	{Err: kernel.ErrItemWorn, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_ITEM_WORN},
	{Err: kernel.ErrLevelTooLow, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_LEVEL_TOO_LOW},
	{Err: kernel.ErrWrongClass, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_WRONG_CLASS},
	{Err: kernel.ErrSTRTooLow, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_STR_TOO_LOW},
	{Err: kernel.ErrDEXTooLow, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_DEX_TOO_LOW},
	{Err: kernel.ErrINTTooLow, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_INT_TOO_LOW},
	{Err: kernel.ErrAGITooLow, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_AGI_TOO_LOW},
	{Err: kernel.ErrVITTooLow, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_VIT_TOO_LOW},
	{Err: kernel.ErrWrongEquipSlot, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_WRONG_EQUIP_SLOT},
	{Err: kernel.ErrNotEquipment, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_NOT_EQUIPMENT},
	{Err: kernel.ErrNothingWorn, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_NOTHING_WORN},
	{Err: kernel.ErrBadBatchSize, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_BATCH_SIZE},
	{Err: kernel.ErrNotEnoughItems, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_NOT_ENOUGH_ITEMS},
	{Err: kernel.ErrNotUsable, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_NOT_USABLE},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_ID},
	{Err: kernel.ErrUnknownClass, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_UNKNOWN_CLASS},
	{Err: kernel.ErrInternal, Code: codes.Internal}, // a panic in the module, already logged and counted there
}
