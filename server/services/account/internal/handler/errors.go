package handler

import (
	"google.golang.org/grpc/codes"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
)

// Errors maps every kernel error the module returns, and a malformed ID, to its gRPC code and contract reason.
var Errors = grpcx.ErrorTable{
	{Err: kernel.ErrAccountNotFound, Code: codes.NotFound, Reason: accountv1.Reason_REASON_ACCOUNT_NOT_FOUND},
	{Err: kernel.ErrUsernameTaken, Code: codes.AlreadyExists, Reason: accountv1.Reason_REASON_USERNAME_TAKEN},
	{Err: kernel.ErrUnauthenticated, Code: codes.Unauthenticated, Reason: accountv1.Reason_REASON_UNAUTHENTICATED},
	{Err: kernel.ErrLoginRateLimited, Code: codes.ResourceExhausted, Reason: accountv1.Reason_REASON_LOGIN_RATE_LIMITED},
	{Err: kernel.ErrBadUsername, Code: codes.InvalidArgument, Reason: accountv1.Reason_REASON_BAD_USERNAME},
	{Err: kernel.ErrBadPassword, Code: codes.InvalidArgument, Reason: accountv1.Reason_REASON_BAD_PASSWORD},
	{Err: kernel.ErrBadControlPreset, Code: codes.InvalidArgument, Reason: accountv1.Reason_REASON_BAD_CONTROL_PRESET},
	{Err: kernel.ErrSessionRace, Code: codes.Aborted, Reason: accountv1.Reason_REASON_SESSION_RACE},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: accountv1.Reason_REASON_BAD_ID},
	{Err: kernel.ErrBusy, Code: codes.ResourceExhausted, Reason: accountv1.Reason_REASON_BUSY},
	{Err: kernel.ErrInternal, Code: codes.Internal}, // a panic in the module, already logged and counted there
}
