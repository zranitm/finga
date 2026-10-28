package handler

import (
	"google.golang.org/grpc/codes"

	worldv1 "finga/api/gen/world/v1"
	"finga/internal/model"
	"finga/pkg/grpcx"
	"finga/services/world/internal/world"
)

// Errors is every error the world module (and parsing a request) can
// return, with its gRPC code and its reason in the contract.
// TestTheTableCoversTheModuleAndTheContract keeps it complete both ways.
var Errors = grpcx.ErrorTable{
	{Err: world.ErrBusy, Code: codes.Unavailable, Reason: worldv1.Reason_REASON_BUSY},
	{Err: world.ErrNoMap, Code: codes.NotFound, Reason: worldv1.Reason_REASON_NO_MAP},
	{Err: world.ErrStopped, Code: codes.Unavailable, Reason: worldv1.Reason_REASON_STOPPED},
	{Err: world.ErrClassUnavailable, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_CLASS_UNAVAILABLE},
	{Err: world.ErrPickupsUnavailable, Code: codes.Unavailable, Reason: worldv1.Reason_REASON_PICKUPS_UNAVAILABLE},
	{Err: world.ErrPickupBusy, Code: codes.ResourceExhausted, Reason: worldv1.Reason_REASON_PICKUP_BUSY},
	{Err: ErrNotSubscribed, Code: codes.FailedPrecondition, Reason: worldv1.Reason_REASON_NOT_SUBSCRIBED},
	{Err: model.ErrBadID, Code: codes.InvalidArgument, Reason: worldv1.Reason_REASON_BAD_ID},
}
