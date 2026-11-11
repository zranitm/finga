package handler

import (
	"google.golang.org/grpc/codes"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/world/internal/model"
)

// Errors maps every module and parsing error to its gRPC code and reason.
var Errors = grpcx.ErrorTable{
	{Err: model.ErrBusy, Code: codes.Unavailable, Reason: worldv1.Reason_REASON_BUSY},
	{Err: model.ErrNoMap, Code: codes.NotFound, Reason: worldv1.Reason_REASON_NO_MAP},
	{Err: model.ErrStopped, Code: codes.Unavailable, Reason: worldv1.Reason_REASON_STOPPED},
	{Err: model.ErrClassUnavailable, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_CLASS_UNAVAILABLE},
	{Err: model.ErrCharacterDead, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_CHARACTER_DEAD},
	{Err: model.ErrPointsChanged, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_POINTS_CHANGED},
	{Err: model.ErrNotEnoughPoints, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_ENOUGH_POINTS},
	{Err: model.ErrNoFreeReset, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NO_FREE_RESET},
	{Err: model.ErrBadPoints, Code: codes.InvalidArgument, Reason: worldv1.Reason_REASON_BAD_POINTS},
	{Err: model.ErrSkillPointsChanged, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_SKILL_POINTS_CHANGED},
	{Err: model.ErrNotEnoughSkillPoints, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_ENOUGH_SKILL_POINTS},
	{Err: model.ErrSkillNotLearnable, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_SKILL_NOT_LEARNABLE},
	{Err: model.ErrBadSkillPoints, Code: codes.InvalidArgument, Reason: worldv1.Reason_REASON_BAD_SKILL_POINTS},
	{Err: model.ErrNpcOutOfReach, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NPC_OUT_OF_REACH},
	{Err: model.ErrQuestUnavailable, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_QUEST_UNAVAILABLE},
	{Err: model.ErrQuestUnmet, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_QUEST_CONDITIONS_UNMET},
	{Err: model.ErrQuestActive, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_QUEST_ALREADY_ACTIVE},
	{Err: model.ErrQuestFinished, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_QUEST_ALREADY_FINISHED},
	{Err: model.ErrTooManyQuests, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_TOO_MANY_QUESTS},
	{Err: model.ErrQuestNotReady, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_QUEST_NOT_READY},
	{Err: model.ErrHPFull, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_HP_FULL},
	{Err: model.ErrMPFull, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_MP_FULL},
	{Err: model.ErrNotUsable, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_USABLE},
	{Err: ErrNotSubscribed, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_SUBSCRIBED},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: worldv1.Reason_REASON_BAD_ID},
}
