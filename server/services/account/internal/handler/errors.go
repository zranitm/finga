package handler

import (
	"google.golang.org/grpc/codes"

	accountv1 "finga/api/gen/account/v1"
	"finga/pkg/grpcx"
	"finga/services/account/internal/account"
)

// Errors is every error the account module can return, with its gRPC code
// and its reason in the contract. A malformed ID is not here: only the
// gateway calls this service, with IDs it got from it, so one is a bug and
// leaves as InvalidArgument without a reason.
// TestTheTableCoversTheModuleAndTheContract keeps it complete both ways.
var Errors = grpcx.ErrorTable{
	{Err: account.ErrNotFound, Code: codes.NotFound, Reason: accountv1.Reason_REASON_ACCOUNT_NOT_FOUND},
	{Err: account.ErrUsernameTaken, Code: codes.AlreadyExists, Reason: accountv1.Reason_REASON_USERNAME_TAKEN},
	{Err: account.ErrUnauthenticated, Code: codes.Unauthenticated, Reason: accountv1.Reason_REASON_UNAUTHENTICATED},
	{Err: account.ErrRateLimited, Code: codes.ResourceExhausted, Reason: accountv1.Reason_REASON_LOGIN_RATE_LIMITED},
	{Err: account.ErrBadUsername, Code: codes.InvalidArgument, Reason: accountv1.Reason_REASON_BAD_USERNAME},
	{Err: account.ErrBadPassword, Code: codes.InvalidArgument, Reason: accountv1.Reason_REASON_BAD_PASSWORD},
	{Err: account.ErrBadPreset, Code: codes.InvalidArgument, Reason: accountv1.Reason_REASON_BAD_CONTROL_PRESET},
	{Err: account.ErrSessionRace, Code: codes.Aborted, Reason: accountv1.Reason_REASON_SESSION_RACE},
}
