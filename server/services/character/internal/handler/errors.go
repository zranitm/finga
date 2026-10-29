package handler

import (
	"google.golang.org/grpc/codes"

	characterv1 "finga/api/gen/character/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/character/internal/model"
)

// Errors is every error the character module (and parsing a request) can
// return, with its gRPC code and its reason in the contract.
// TestTheTableCoversTheModuleAndTheContract keeps it complete both ways.
var Errors = grpcx.ErrorTable{
	{Err: model.ErrNotFound, Code: codes.NotFound, Reason: characterv1.Reason_REASON_CHARACTER_NOT_FOUND},
	{Err: model.ErrNameTaken, Code: codes.AlreadyExists, Reason: characterv1.Reason_REASON_NAME_TAKEN},
	{Err: model.ErrBadName, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_CHARACTER_NAME},
	{Err: model.ErrClassUnavailable, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_CLASS_UNAVAILABLE},
	{Err: model.ErrSlotsFull, Code: codes.FailedPrecondition, Reason: characterv1.Reason_REASON_CHARACTER_SLOTS_FULL},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_BAD_ID},
	{Err: kernel.ErrUnknownClass, Code: codes.InvalidArgument, Reason: characterv1.Reason_REASON_UNKNOWN_CLASS},
}
