package handler

import (
	"google.golang.org/grpc/codes"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/model"
	"finga/pkg/grpcx"
	"finga/services/character/internal/character"
)

// Errors is every error the character module (and parsing a request) can
// return, with its gRPC code and its reason in the contract.
// TestTheTableCoversTheModuleAndTheContract keeps it complete both ways.
var Errors = grpcx.ErrorTable{
	{Err: character.ErrNotFound, Code: codes.NotFound, Reason: characterv1.Reason_REASON_CHARACTER_NOT_FOUND},
	{Err: character.ErrNameTaken, Code: codes.AlreadyExists, Reason: characterv1.Reason_REASON_NAME_TAKEN},
	{Err: character.ErrBadName, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_CHARACTER_NAME},
	{Err: character.ErrClassUnavailable, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_CLASS_UNAVAILABLE},
	{Err: character.ErrSlotsFull, Code: codes.FailedPrecondition, Reason: characterv1.Reason_REASON_CHARACTER_SLOTS_FULL},
	{Err: model.ErrBadID, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_ID},
	{Err: model.ErrUnknownClass, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_UNKNOWN_CLASS},
}
