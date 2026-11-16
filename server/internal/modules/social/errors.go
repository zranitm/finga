package social

import (
	"finga/internal/facade"
	"finga/internal/kernel"
	"finga/modules/social/internal/model"
)

// errorTable maps every model error to the kernel error callers see.
var errorTable = facade.Errors{
	{Model: model.ErrNotLeader, Kernel: kernel.ErrNotPartyLeader},
	{Model: model.ErrFull, Kernel: kernel.ErrPartyFull},
	{Model: model.ErrInParty, Kernel: kernel.ErrAlreadyInParty},
	{Model: model.ErrTargetInParty, Kernel: kernel.ErrTargetInParty},
	{Model: model.ErrNoParty, Kernel: kernel.ErrNoParty},
	{Model: model.ErrNotMember, Kernel: kernel.ErrNotPartyMember},
	{Model: model.ErrNotHere, Kernel: kernel.ErrTargetNotOnMap},
	{Model: model.ErrAlreadyAsked, Kernel: kernel.ErrAlreadyInvited},
	{Model: model.ErrInviteExpired, Kernel: kernel.ErrInviteExpired},
	{Model: model.ErrSelf, Kernel: kernel.ErrInviteSelf},
	{Model: model.ErrEmpty, Kernel: kernel.ErrChatEmpty},
	{Model: model.ErrTooLong, Kernel: kernel.ErrChatTooLong},
	{Model: model.ErrChannel, Kernel: kernel.ErrBadChatChannel},
	{Model: model.ErrChatNoParty, Kernel: kernel.ErrChatNoParty},
	{Model: model.ErrRate, Kernel: kernel.ErrChatRateLimited},
}
