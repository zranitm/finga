package world

import (
	"finga/internal/facade"
	"finga/internal/kernel"
	"finga/modules/world/internal/model"
)

// errorTable maps every model error to the kernel error callers see.
var errorTable = facade.Errors{
	{Model: model.ErrBusy, Kernel: kernel.ErrMapBusy},
	{Model: model.ErrNoMap, Kernel: kernel.ErrNoMap},
	{Model: model.ErrStopped, Kernel: kernel.ErrWorldStopped},
	{Model: model.ErrClassUnavailable, Kernel: kernel.ErrClassUnavailable},
	{Model: model.ErrCharacterDead, Kernel: kernel.ErrCharacterDead},
	{Model: model.ErrPointsChanged, Kernel: kernel.ErrPointsChanged},
	{Model: model.ErrNotEnoughPoints, Kernel: kernel.ErrNotEnoughPoints},
	{Model: model.ErrNoFreeReset, Kernel: kernel.ErrNoFreeReset},
	{Model: model.ErrBadPoints, Kernel: kernel.ErrBadPoints},
	{Model: model.ErrSkillPointsChanged, Kernel: kernel.ErrSkillPointsChanged},
	{Model: model.ErrNotEnoughSkillPoints, Kernel: kernel.ErrNotEnoughSkillPoints},
	{Model: model.ErrSkillNotLearnable, Kernel: kernel.ErrSkillNotLearnable},
	{Model: model.ErrBadSkillPoints, Kernel: kernel.ErrBadSkillPoints},
	{Model: model.ErrSkillNotLearned, Kernel: kernel.ErrSkillNotLearned},
	{Model: model.ErrSkillPassive, Kernel: kernel.ErrSkillPassive},
	{Model: model.ErrNotInTown, Kernel: kernel.ErrNotInTown},
	{Model: model.ErrSkillCoolingDown, Kernel: kernel.ErrSkillCoolingDown},
	{Model: model.ErrNotEnoughEnergy, Kernel: kernel.ErrNotEnoughEnergy},
	{Model: model.ErrSkillWaiting, Kernel: kernel.ErrSkillWaiting},
	{Model: model.ErrPromotionUnmet, Kernel: kernel.ErrPromotionConditionsUnmet},
	{Model: model.ErrNotYourNextClass, Kernel: kernel.ErrNotYourNextClass},
	{Model: model.ErrNpcOutOfReach, Kernel: kernel.ErrNpcOutOfReach},
	{Model: model.ErrQuestUnavailable, Kernel: kernel.ErrQuestUnavailable},
	{Model: model.ErrQuestUnmet, Kernel: kernel.ErrQuestConditionsUnmet},
	{Model: model.ErrQuestActive, Kernel: kernel.ErrQuestAlreadyActive},
	{Model: model.ErrQuestFinished, Kernel: kernel.ErrQuestAlreadyFinished},
	{Model: model.ErrTooManyQuests, Kernel: kernel.ErrTooManyQuests},
	{Model: model.ErrQuestNotReady, Kernel: kernel.ErrQuestNotReady},
	{Model: model.ErrHPFull, Kernel: kernel.ErrHPFull},
	{Model: model.ErrNotUsable, Kernel: kernel.ErrNotUsable},
}
