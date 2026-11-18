package pvp

import (
	model "finga/internal/model/pvp"
	"finga/internal/shared/facade"
	"finga/internal/shared/kernel"
)

// errorTable maps every model error to the kernel error callers see; the world's come as its kernel errors already.
var errorTable = facade.Errors{
	{Model: model.ErrUnknownMode, Kernel: kernel.ErrPvpUnknownMode},
	{Model: model.ErrAlreadyQueued, Kernel: kernel.ErrPvpAlreadyQueued},
	{Model: model.ErrNotQueued, Kernel: kernel.ErrPvpNotQueued},
	{Model: model.ErrOfferGone, Kernel: kernel.ErrPvpOfferGone},
	{Model: model.ErrInMatch, Kernel: kernel.ErrPvpInMatch},
}
