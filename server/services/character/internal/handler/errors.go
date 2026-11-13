package handler

import (
	"google.golang.org/grpc/codes"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
)

// Errors maps every kernel error the module returns, and a malformed ID, to its gRPC code and contract reason.
var Errors = grpcx.ErrorTable{
	{Err: kernel.ErrCharacterNotFound, Code: codes.NotFound, Reason: characterv1.Reason_REASON_CHARACTER_NOT_FOUND},
	{Err: kernel.ErrNameTaken, Code: codes.AlreadyExists, Reason: characterv1.Reason_REASON_NAME_TAKEN},
	{Err: kernel.ErrBadCharacterName, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_CHARACTER_NAME},
	{Err: kernel.ErrClassUnavailable, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_CLASS_UNAVAILABLE},
	{Err: kernel.ErrCharacterSlotsFull, Code: codes.FailedPrecondition, Reason: characterv1.Reason_REASON_CHARACTER_SLOTS_FULL},
	{Err: kernel.ErrBadQuest, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_QUEST},
	{Err: kernel.ErrBadSkill, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_SKILL},
	{Err: kernel.ErrBadSkillTimer, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_SKILL_TIMER},
	{Err: kernel.ErrBadSkillBar, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_SKILL_BAR},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_ID},
	{Err: kernel.ErrInternal, Code: codes.Internal}, // a panic in the module, already logged and counted there
}
