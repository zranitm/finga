package clients

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"

	accountv1 "finga/api/gen/account/v1"
	"finga/internal/kernel"
	"finga/pkg/grpcx"
	"finga/services/gateway/internal/model"
)

// SessionWatcher is what the stream of ended sessions needs of the account
// service's generated client.
type SessionWatcher interface {
	WatchSessions(ctx context.Context, in *accountv1.WatchSessionsRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[accountv1.WatchSessionsResponse], error)
}

// SessionEnds is the application's SessionEnds over the account service.
type SessionEnds struct {
	c   SessionWatcher
	log *slog.Logger
}

// NewSessionEnds adapts the account client; log hears the stream's breaks.
func NewSessionEnds(c SessionWatcher, log *slog.Logger) SessionEnds {
	return SessionEnds{c: c, log: log}
}

// FollowSessionEnds follows the ended sessions until ctx ends.
func (s SessionEnds) FollowSessionEnds(ctx context.Context, gateway string, deliver func(model.SessionEnd)) {
	grpcx.Follow(ctx, func(ctx context.Context) (grpc.ServerStreamingClient[accountv1.WatchSessionsResponse], error) {
		return s.c.WatchSessions(ctx, &accountv1.WatchSessionsRequest{GatewayId: gateway})
	}, func(m *accountv1.WatchSessionsResponse) {
		switch {
		case m.GetSessionRevoked() != nil:
			if id, err := kernel.ParseSessionID(m.GetSessionRevoked().GetSessionId()); err == nil {
				deliver(model.SessionEnd{Session: id})
			}
		case m.GetAccountRevoked() != nil:
			ev := m.GetAccountRevoked()
			if id, err := kernel.ParseAccountID(ev.GetAccountId()); err == nil {
				deliver(model.SessionEnd{Account: id, NewLogin: ev.GetReason() == accountv1.RevokeReason_REVOKE_REASON_NEW_LOGIN})
			}
		}
	}, grpcx.FollowOptions{OnBreak: func(err error) {
		s.log.WarnContext(ctx, "session stream broke; subscribing again", "err", err)
	}})
}
