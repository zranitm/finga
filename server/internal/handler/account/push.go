package account

import (
	clientv1 "finga/api/gen/client/v1"
	"finga/internal/handler/transport"
	model "finga/internal/model/gateway"
)

// Encoders registers the account's part of the Welcome: the token, the clock and the waypoints a join answers with.
func Encoders(e *transport.Encoders) error {
	e.WelcomeSection(func(w model.Welcome, out *clientv1.Welcome) {
		out.Token, out.MapId, out.ServerTime, out.TickRateHz, out.Waypoints = w.Token, w.MapID, w.ServerTime.UnixMilli(), uint32(w.TickRateHz), w.Waypoints
	})
	return nil
}
