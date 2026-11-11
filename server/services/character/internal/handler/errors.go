package handler

import (
	"google.golang.org/grpc/codes"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/character/internal/model"
)

// Errors maps every module and parsing error to its gRPC code and reason, or none for a retry.
var Errors = grpcx.ErrorTable{
	{Err: model.ErrNotFound, Code: codes.NotFound, Reason: characterv1.Reason_REASON_CHARACTER_NOT_FOUND},
	{Err: model.ErrNameTaken, Code: codes.AlreadyExists, Reason: characterv1.Reason_REASON_NAME_TAKEN},
	{Err: model.ErrBadName, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_CHARACTER_NAME},
	{Err: model.ErrClassUnavailable, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_CLASS_UNAVAILABLE},
	{Err: model.ErrSlotsFull, Code: codes.FailedPrecondition, Reason: characterv1.Reason_REASON_CHARACTER_SLOTS_FULL},
	{Err: model.ErrBadQuest, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_QUEST},
	{Err: model.ErrBadSkill, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_SKILL},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_ID},
}
