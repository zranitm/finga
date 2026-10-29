// Package app is the social service's composition root (Low-level design,
// page 10): the party and chat modules, in memory, and the gRPC handler
// that is the party module's notifier, on a host. It is a package of its
// own, next to the main, because the end-to-end tests build the service
// exactly as production does.
package app

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/host"
	"finga/services/social/config"
	"finga/services/social/internal/chat"
	"finga/services/social/internal/handler"
	"finga/services/social/internal/party"
)

// Build wires the social service from configuration. It needs no database:
// parties live in memory (TD-19), and a restart forgets them.
func Build(_ context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	parties := party.NewService(party.Options{})
	srv := handler.New(parties, chat.NewService(parties, chat.Options{}), uuid.NewString())
	parties.Notify(srv)
	h := host.New(host.Options{
		Name:     "social",
		Settings: cfg.Host(),
		Logger:   log,
		Registry: reg,
		Errors:   handler.Errors,
	})
	socialv1.RegisterSocialServiceServer(h.GRPC, srv)
	return h, nil
}
