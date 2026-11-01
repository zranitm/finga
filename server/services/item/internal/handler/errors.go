package handler

import (
	"google.golang.org/grpc/codes"

	itemv1 "finga/api/gen/item/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/item/internal/model"
)

// Errors maps every module and request-parsing error to its gRPC code and contract reason.
var Errors = grpcx.ErrorTable{
	{Err: model.ErrNotFound, Code: codes.NotFound, Reason: itemv1.Reason_REASON_ITEM_NOT_FOUND},
	{Err: model.ErrSlotTaken, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_SLOT_TAKEN},
	{Err: model.ErrDifferentItem, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_SLOT_HOLDS_OTHER_ITEM},
	{Err: model.ErrInventoryFull, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_INVENTORY_FULL},
	{Err: model.ErrKeyReused, Code: codes.FailedPrecondition, Reason: itemv1.Reason_REASON_IDEMPOTENCY_KEY_REUSED},
	{Err: model.ErrBadQuantity, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_QUANTITY},
	{Err: model.ErrBadSlot, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_SLOT},
	{Err: model.ErrWrongTab, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_WRONG_TAB},
	{Err: model.ErrUnknownItem, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_UNKNOWN_ITEM},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_BAD_ID},
	{Err: kernel.ErrUnknownClass, Code: codes.InvalidArgument, Reason: itemv1.Reason_REASON_UNKNOWN_CLASS},
}
