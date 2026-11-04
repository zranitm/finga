package handler

import (
	"google.golang.org/grpc/codes"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/account/internal/model"
)

// Errors maps every module and parsing error to its gRPC code and contract reason.
var Errors = grpcx.ErrorTable{
	{Err: model.ErrNotFound, Code: codes.NotFound, Reason: accountv1.Reason_REASON_ACCOUNT_NOT_FOUND},
	{Err: model.ErrUsernameTaken, Code: codes.AlreadyExists, Reason: accountv1.Reason_REASON_USERNAME_TAKEN},
	{Err: model.ErrUnauthenticated, Code: codes.Unauthenticated, Reason: accountv1.Reason_REASON_UNAUTHENTICATED},
	{Err: model.ErrRateLimited, Code: codes.ResourceExhausted, Reason: accountv1.Reason_REASON_LOGIN_RATE_LIMITED},
	{Err: model.ErrBadUsername, Code: codes.InvalidArgument, Reason: accountv1.Reason_REASON_BAD_USERNAME},
	{Err: model.ErrBadPassword, Code: codes.InvalidArgument, Reason: accountv1.Reason_REASON_BAD_PASSWORD},
	{Err: model.ErrBadPreset, Code: codes.InvalidArgument, Reason: accountv1.Reason_REASON_BAD_CONTROL_PRESET},
	{Err: model.ErrSessionRace, Code: codes.Aborted, Reason: accountv1.Reason_REASON_SESSION_RACE},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: accountv1.Reason_REASON_BAD_ID},
}
