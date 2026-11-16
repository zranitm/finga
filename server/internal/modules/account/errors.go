package account

import (
	"finga/internal/facade"
	"finga/internal/kernel"
	"finga/modules/account/internal/model"
)

// errorTable maps every model error to the kernel error callers see.
var errorTable = facade.Errors{
	{Model: model.ErrNotFound, Kernel: kernel.ErrAccountNotFound},
	{Model: model.ErrUsernameTaken, Kernel: kernel.ErrUsernameTaken},
	{Model: model.ErrUnauthenticated, Kernel: kernel.ErrUnauthenticated},
	{Model: model.ErrRateLimited, Kernel: kernel.ErrLoginRateLimited},
	{Model: model.ErrBadUsername, Kernel: kernel.ErrBadUsername},
	{Model: model.ErrBadPassword, Kernel: kernel.ErrBadPassword},
	{Model: model.ErrBadPreset, Kernel: kernel.ErrBadControlPreset},
	{Model: model.ErrSessionRace, Kernel: kernel.ErrSessionRace},
	{Model: model.ErrBusy, Kernel: kernel.ErrBusy},
}
