package handler

import (
	"google.golang.org/grpc/codes"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/world/internal/model"
)

// Errors is every error the world module (and parsing a request) can
// return, with its gRPC code and its reason in the contract.
// TestTheTableCoversTheModuleAndTheContract keeps it complete both ways.
var Errors = grpcx.ErrorTable{
	{Err: model.ErrBusy, Code: codes.Unavailable, Reason: worldv1.Reason_REASON_BUSY},
	{Err: model.ErrNoMap, Code: codes.NotFound, Reason: worldv1.Reason_REASON_NO_MAP},
	{Err: model.ErrStopped, Code: codes.Unavailable, Reason: worldv1.Reason_REASON_STOPPED},
	{Err: model.ErrClassUnavailable, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_CLASS_UNAVAILABLE},
	{Err: model.ErrPickupsUnavailable, Code: codes.Unavailable, Reason: worldv1.Reason_REASON_PICKUPS_UNAVAILABLE},
	{Err: model.ErrPickupBusy, Code: codes.ResourceExhausted, Reason: worldv1.Reason_REASON_PICKUP_BUSY},
	{Err: ErrNotSubscribed, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_SUBSCRIBED},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: worldv1.Reason_REASON_BAD_ID},
}
