package handler

import (
	"google.golang.org/grpc/codes"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
)

// Errors maps every kernel error the module returns, and a malformed ID, to its gRPC code and contract reason.
var Errors = grpcx.ErrorTable{
	{Err: kernel.ErrNotPartyLeader, Code: codes.FailedPrecondition, Reason: socialv1.Reason_REASON_NOT_PARTY_LEADER},
	{Err: kernel.ErrPartyFull, Code: codes.FailedPrecondition, Reason: socialv1.Reason_REASON_PARTY_FULL},
	{Err: kernel.ErrAlreadyInParty, Code: codes.FailedPrecondition, Reason: socialv1.Reason_REASON_ALREADY_IN_PARTY},
	{Err: kernel.ErrTargetInParty, Code: codes.FailedPrecondition, Reason: socialv1.Reason_REASON_TARGET_IN_PARTY},
	{Err: kernel.ErrNoParty, Code: codes.NotFound, Reason: socialv1.Reason_REASON_NO_PARTY},
	{Err: kernel.ErrNotPartyMember, Code: codes.NotFound, Reason: socialv1.Reason_REASON_NOT_PARTY_MEMBER},
	{Err: kernel.ErrTargetNotOnMap, Code: codes.NotFound, Reason: socialv1.Reason_REASON_TARGET_NOT_ON_MAP},
	{Err: kernel.ErrAlreadyInvited, Code: codes.AlreadyExists, Reason: socialv1.Reason_REASON_ALREADY_INVITED},
	{Err: kernel.ErrInviteExpired, Code: codes.NotFound, Reason: socialv1.Reason_REASON_INVITE_EXPIRED},
	{Err: kernel.ErrInviteSelf, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_INVITE_SELF},
	{Err: kernel.ErrChatEmpty, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_CHAT_EMPTY},
	{Err: kernel.ErrChatTooLong, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_CHAT_TOO_LONG},
	{Err: kernel.ErrBadChatChannel, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_BAD_CHAT_CHANNEL},
	{Err: kernel.ErrChatNoParty, Code: codes.NotFound, Reason: socialv1.Reason_REASON_CHAT_NO_PARTY},
	{Err: kernel.ErrChatRateLimited, Code: codes.ResourceExhausted, Reason: socialv1.Reason_REASON_CHAT_RATE_LIMITED},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_BAD_ID},
	{Err: kernel.ErrInternal, Code: codes.Internal}, // a panic in the module, already logged and counted there
}
