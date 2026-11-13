package handler

import (
	"google.golang.org/grpc/codes"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
)

// Errors maps every kernel error the module returns, and a malformed request, to its gRPC code and contract reason.
var Errors = grpcx.ErrorTable{
	{Err: kernel.ErrMapBusy, Code: codes.Unavailable, Reason: worldv1.Reason_REASON_BUSY},
	{Err: kernel.ErrNoMap, Code: codes.NotFound, Reason: worldv1.Reason_REASON_NO_MAP},
	{Err: kernel.ErrWorldStopped, Code: codes.Unavailable, Reason: worldv1.Reason_REASON_STOPPED},
	{Err: kernel.ErrClassUnavailable, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_CLASS_UNAVAILABLE},
	{Err: kernel.ErrCharacterDead, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_CHARACTER_DEAD},
	{Err: kernel.ErrPointsChanged, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_POINTS_CHANGED},
	{Err: kernel.ErrNotEnoughPoints, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_ENOUGH_POINTS},
	{Err: kernel.ErrNoFreeReset, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NO_FREE_RESET},
	{Err: kernel.ErrBadPoints, Code: codes.InvalidArgument, Reason: worldv1.Reason_REASON_BAD_POINTS},
	{Err: kernel.ErrSkillPointsChanged, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_SKILL_POINTS_CHANGED},
	{Err: kernel.ErrNotEnoughSkillPoints, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_ENOUGH_SKILL_POINTS},
	{Err: kernel.ErrSkillNotLearnable, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_SKILL_NOT_LEARNABLE},
	{Err: kernel.ErrBadSkillPoints, Code: codes.InvalidArgument, Reason: worldv1.Reason_REASON_BAD_SKILL_POINTS},
	{Err: kernel.ErrSkillNotLearned, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_SKILL_NOT_LEARNED},
	{Err: kernel.ErrSkillPassive, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_SKILL_PASSIVE},
	{Err: kernel.ErrNotInTown, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_IN_TOWN},
	{Err: kernel.ErrSkillCoolingDown, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_SKILL_COOLING_DOWN},
	{Err: kernel.ErrNotEnoughMP, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_ENOUGH_MP},
	{Err: kernel.ErrSkillWaiting, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_SKILL_WAITING},
	{Err: kernel.ErrPromotionConditionsUnmet, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_PROMOTION_CONDITIONS_UNMET},
	{Err: kernel.ErrNotYourNextClass, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_YOUR_NEXT_CLASS},
	{Err: kernel.ErrNpcOutOfReach, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NPC_OUT_OF_REACH},
	{Err: kernel.ErrQuestUnavailable, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_QUEST_UNAVAILABLE},
	{Err: kernel.ErrQuestConditionsUnmet, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_QUEST_CONDITIONS_UNMET},
	{Err: kernel.ErrQuestAlreadyActive, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_QUEST_ALREADY_ACTIVE},
	{Err: kernel.ErrQuestAlreadyFinished, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_QUEST_ALREADY_FINISHED},
	{Err: kernel.ErrTooManyQuests, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_TOO_MANY_QUESTS},
	{Err: kernel.ErrQuestNotReady, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_QUEST_NOT_READY},
	{Err: kernel.ErrHPFull, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_HP_FULL},
	{Err: kernel.ErrMPFull, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_MP_FULL},
	{Err: kernel.ErrNotUsable, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_USABLE},
	{Err: kernel.ErrNotSubscribed, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_SUBSCRIBED},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: worldv1.Reason_REASON_BAD_ID},
	{Err: kernel.ErrInternal, Code: codes.Internal}, // a panic in the module, already logged and counted there
}
