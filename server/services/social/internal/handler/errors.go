package handler

import (
	"google.golang.org/grpc/codes"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/social/internal/chat"
	"finga/services/social/internal/party"
)

// Errors is every error the party and chat modules (and parsing a request)
// can return, with its gRPC code and its reason in the contract.
// TestTheTableCoversTheModulesAndTheContract keeps it complete both ways.
var Errors = grpcx.ErrorTable{
	{Err: party.ErrNotLeader, Code: codes.FailedPrecondition, Reason: socialv1.Reason_REASON_NOT_PARTY_LEADER},
	{Err: party.ErrFull, Code: codes.FailedPrecondition, Reason: socialv1.Reason_REASON_PARTY_FULL},
	{Err: party.ErrInParty, Code: codes.FailedPrecondition, Reason: socialv1.Reason_REASON_ALREADY_IN_PARTY},
	{Err: party.ErrNoParty, Code: codes.NotFound, Reason: socialv1.Reason_REASON_NO_PARTY},
	{Err: party.ErrNotMember, Code: codes.NotFound, Reason: socialv1.Reason_REASON_NOT_PARTY_MEMBER},
	{Err: party.ErrNotHere, Code: codes.NotFound, Reason: socialv1.Reason_REASON_TARGET_NOT_ON_MAP},
	{Err: party.ErrAlreadyAsked, Code: codes.AlreadyExists, Reason: socialv1.Reason_REASON_ALREADY_INVITED},
	{Err: party.ErrInviteExpired, Code: codes.NotFound, Reason: socialv1.Reason_REASON_INVITE_EXPIRED},
	{Err: party.ErrSelf, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_INVITE_SELF},
	{Err: chat.ErrEmpty, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_CHAT_EMPTY},
	{Err: chat.ErrTooLong, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_CHAT_TOO_LONG},
	{Err: chat.ErrChannel, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_BAD_CHAT_CHANNEL},
	{Err: chat.ErrNoParty, Code: codes.NotFound, Reason: socialv1.Reason_REASON_CHAT_NO_PARTY},
	{Err: chat.ErrRate, Code: codes.ResourceExhausted, Reason: socialv1.Reason_REASON_CHAT_RATE_LIMITED},
	{Err: chat.ErrNoParties, Code: codes.Unavailable, Reason: socialv1.Reason_REASON_PARTY_CHAT_UNAVAILABLE},
	{Err: kernel.ErrBadID, Code: codes.InvalidArgument, Reason: socialv1.Reason_REASON_BAD_ID},
}
