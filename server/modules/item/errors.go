package item

import (
	"finga/internal/facade"
	"finga/internal/kernel"
	"finga/modules/item/internal/model"
)

// errorTable maps every model error to the kernel error callers see.
var errorTable = facade.Errors{
	{Model: model.ErrNotFound, Kernel: kernel.ErrItemNotFound},
	{Model: model.ErrSlotTaken, Kernel: kernel.ErrSlotTaken},
	{Model: model.ErrDifferentItem, Kernel: kernel.ErrSlotHoldsOtherItem},
	{Model: model.ErrStackFull, Kernel: kernel.ErrStackFull},
	{Model: model.ErrEquipTabFull, Kernel: kernel.ErrEquipTabFull},
	{Model: model.ErrUseTabFull, Kernel: kernel.ErrUseTabFull},
	{Model: model.ErrEtcTabFull, Kernel: kernel.ErrEtcTabFull},
	{Model: model.ErrKeyReused, Kernel: kernel.ErrIdempotencyKeyReused},
	{Model: model.ErrBadKey, Kernel: kernel.ErrBadIdempotencyKey},
	{Model: model.ErrBadCause, Kernel: kernel.ErrBadCause},
	{Model: model.ErrBadQuantity, Kernel: kernel.ErrBadQuantity},
	{Model: model.ErrBadSlot, Kernel: kernel.ErrBadSlot},
	{Model: model.ErrWrongTab, Kernel: kernel.ErrWrongTab},
	{Model: model.ErrUnknownItem, Kernel: kernel.ErrUnknownItem},
	{Model: model.ErrBadRoll, Kernel: kernel.ErrBadRoll},
	{Model: model.ErrItemWorn, Kernel: kernel.ErrItemWorn},
	{Model: model.ErrLevelTooLow, Kernel: kernel.ErrLevelTooLow},
	{Model: model.ErrWrongClass, Kernel: kernel.ErrWrongClass},
	{Model: model.ErrSTRTooLow, Kernel: kernel.ErrSTRTooLow},
	{Model: model.ErrDEXTooLow, Kernel: kernel.ErrDEXTooLow},
	{Model: model.ErrINTTooLow, Kernel: kernel.ErrINTTooLow},
	{Model: model.ErrAGITooLow, Kernel: kernel.ErrAGITooLow},
	{Model: model.ErrVITTooLow, Kernel: kernel.ErrVITTooLow},
	{Model: model.ErrWrongEquipSlot, Kernel: kernel.ErrWrongEquipSlot},
	{Model: model.ErrNotEquipment, Kernel: kernel.ErrNotEquipment},
	{Model: model.ErrNothingWorn, Kernel: kernel.ErrNothingWorn},
	{Model: model.ErrBadBatch, Kernel: kernel.ErrBadBatchSize},
	{Model: model.ErrNotEnoughItems, Kernel: kernel.ErrNotEnoughItems},
	{Model: model.ErrNotUsable, Kernel: kernel.ErrNotUsable},
}
