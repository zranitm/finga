package handler

import (
	"google.golang.org/grpc/codes"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/social/internal/model"
)

// Errors is every error the party and chat modules (and parsing a request)
// can return, with its gRPC code and its reason in the contract.
// TestTheTableCoversTheModulesAndTheContract keeps it complete both ways.
var Errors = grpcx.ErrorTable{
	{Err: model.ErrNotLeader, Code: codes.FailedPrecondition, Reason: socialv1.Reason_REASON_NOT_PARTY_LEADER},
	{Err: model.ErrFull, Code: codes.FailedPrecondition, Reason: socialv1.Reason_REASON_PARTY_FULL},
	{Err: model.ErrInParty, Code: codes.FailedPrecondition, Reason: socialv1.Reason_REASON_ALREADY_IN_PARTY},
	{Err: model.ErrNoParty, Code: codes.NotFound, Reason: socialv1.Reason_REASON_NO_PARTY},
	{Err: model.ErrNotMember, Code: codes.NotFound, Reason: socialv1.Reason_REASON_NOT_PARTY_MEMBER},
	{Err: model.ErrNotHere, Code: codes.NotFound, Reason: socialv1.Reason_REASON_TARGET_NOT_ON_MAP},
	{Err: model.ErrAlreadyAsked, Code: codes.AlreadyExists, Reason: socialv1.Reason_REASON_ALREADY_INVITED},
	{Err: model.ErrInviteExpired, Code: codes.NotFound, Reason: socialv1.Reason_REASON_INVITE_EXPIRED},
	{Err: model.ErrSelf, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_INVITE_SELF},
	{Err: model.ErrEmpty, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_CHAT_EMPTY},
	{Err: model.ErrTooLong, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_CHAT_TOO_LONG},
	{Err: model.ErrChannel, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_BAD_CHAT_CHANNEL},
	{Err: model.ErrChatNoParty, Code: codes.NotFound, Reason: socialv1.Reason_REASON_CHAT_NO_PARTY},
	{Err: model.ErrRate, Code: codes.ResourceExhausted, Reason: socialv1.Reason_REASON_CHAT_RATE_LIMITED},
	{Err: model.ErrNoParties, Code: codes.Unavailable, Reason: socialv1.Reason_REASON_PARTY_CHAT_UNAVAILABLE},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_BAD_ID},
}
