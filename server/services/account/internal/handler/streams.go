package handler

import (
	"context"

	"github.com/google/uuid"

	accountv1 "finga/api/gen/account/v1"
	"finga/pkg/hub"
	"finga/services/account/internal/application"
)

// watchQueue bounds the session ends queued for a slow gateway before its stream is dropped.
const watchQueue = 1024

// Streams are the gateways' session-end streams, and the module's notifier.
type Streams struct {
	bootID   string
	watchers *hub.Hub[*accountv1.WatchSessionsResponse]
}

var _ application.Notifier = (*Streams)(nil)

// NewStreams makes the streams; a per-process bootID lets a gateway tell a restart from a broken stream.
func NewStreams(bootID string) *Streams {
	return &Streams{bootID: bootID, watchers: hub.New[*accountv1.WatchSessionsResponse](watchQueue)}
}

// SessionRevoked is broadcast to every gateway.
func (s *Streams) SessionRevoked(_ context.Context, id uuid.UUID, reason string) {
	s.watchers.Broadcast(&accountv1.WatchSessionsResponse{Event: &accountv1.WatchSessionsResponse_SessionRevoked{
		SessionRevoked: &accountv1.SessionRevoked{SessionId: id.String(), Reason: revokeReason(reason)},
	}})
}

// AccountRevoked is broadcast to every gateway.
func (s *Streams) AccountRevoked(_ context.Context, id uuid.UUID, reason string) {
	s.watchers.Broadcast(&accountv1.WatchSessionsResponse{Event: &accountv1.WatchSessionsResponse_AccountRevoked{
		AccountRevoked: &accountv1.AccountRevoked{AccountId: id.String(), Reason: revokeReason(reason)},
	}})
}

func revokeReason(reason string) accountv1.RevokeReason {
	switch reason {
	case application.ReasonTokenReuse:
		return accountv1.RevokeReason_REVOKE_REASON_TOKEN_REUSE
	case application.ReasonLogout:
		return accountv1.RevokeReason_REVOKE_REASON_LOGOUT
	case application.ReasonNewLogin:
		return accountv1.RevokeReason_REVOKE_REASON_NEW_LOGIN
	}
	return accountv1.RevokeReason_REVOKE_REASON_UNSPECIFIED
}
