package character

import (
	"finga/internal/facade"
	"finga/internal/kernel"
	"finga/services/character/internal/model"
)

// errorTable maps every model error to the kernel error callers see.
var errorTable = facade.Errors{
	{Model: model.ErrNotFound, Kernel: kernel.ErrCharacterNotFound},
	{Model: model.ErrNameTaken, Kernel: kernel.ErrNameTaken},
	{Model: model.ErrBadName, Kernel: kernel.ErrBadCharacterName},
	{Model: model.ErrClassUnavailable, Kernel: kernel.ErrClassUnavailable},
	{Model: model.ErrSlotsFull, Kernel: kernel.ErrCharacterSlotsFull},
	{Model: model.ErrBadQuest, Kernel: kernel.ErrBadQuest},
	{Model: model.ErrBadSkill, Kernel: kernel.ErrBadSkill},
	{Model: model.ErrBadSkillTimer, Kernel: kernel.ErrBadSkillTimer},
	{Model: model.ErrBadSkillBar, Kernel: kernel.ErrBadSkillBar},
}
