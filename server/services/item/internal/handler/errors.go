package handler

import (
	"google.golang.org/grpc/codes"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/item/internal/model"
)

// Errors maps every module and request-parsing error to its gRPC code and contract reason, or none for a retry.
var Errors = grpcx.ErrorTable{
	{Err: model.ErrNotFound, Code: codes.NotFound, Reason: itemv1.Reason_REASON_ITEM_NOT_FOUND},
	{Err: model.ErrSlotTaken, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_SLOT_TAKEN},
	{Err: model.ErrDifferentItem, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_SLOT_HOLDS_OTHER_ITEM},
	{Err: model.ErrStackFull, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_STACK_FULL},
	{Err: model.ErrEquipTabFull, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_EQUIP_TAB_FULL},
	{Err: model.ErrUseTabFull, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_USE_TAB_FULL},
	{Err: model.ErrEtcTabFull, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_ETC_TAB_FULL},
	{Err: model.ErrKeyReused, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED},
	{Err: model.ErrBadKey, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_IDEMPOTENCY_KEY},
	{Err: model.ErrBadCause, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_CAUSE},
	{Err: model.ErrBadQuantity, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_QUANTITY},
	{Err: model.ErrBadSlot, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_SLOT},
	{Err: model.ErrWrongTab, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_WRONG_TAB},
	{Err: model.ErrUnknownItem, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_UNKNOWN_ITEM},
	{Err: model.ErrBadRoll, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_ROLL},
	{Err: model.ErrItemWorn, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_ITEM_WORN},
	{Err: model.ErrLevelTooLow, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_LEVEL_TOO_LOW},
	{Err: model.ErrWrongClass, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_WRONG_CLASS},
	{Err: model.ErrSTRTooLow, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_STR_TOO_LOW},
	{Err: model.ErrDEXTooLow, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_DEX_TOO_LOW},
	{Err: model.ErrINTTooLow, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_INT_TOO_LOW},
	{Err: model.ErrAGITooLow, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_AGI_TOO_LOW},
	{Err: model.ErrVITTooLow, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_VIT_TOO_LOW},
	{Err: model.ErrWrongEquipSlot, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_WRONG_EQUIP_SLOT},
	{Err: model.ErrNotEquipment, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_NOT_EQUIPMENT},
	{Err: model.ErrNothingWorn, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_NOTHING_WORN},
	{Err: model.ErrBadBatch, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_BATCH_SIZE},
	{Err: model.ErrNotEnoughItems, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_NOT_ENOUGH_ITEMS},
	{Err: model.ErrNotUsable, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_NOT_USABLE},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_ID},
	{Err: kernel.ErrUnknownClass, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_UNKNOWN_CLASS},
}
