// Package app is the social service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/host"
	"finga/pkg/uuidv7"
	"finga/services/social/config"
	"finga/services/social/internal/application"
	"finga/services/social/internal/handler"
	"finga/services/social/internal/repository"
)

// Build wires the social service; parties live in memory, so a restart forgets them.
func Build(_ context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	parties := application.NewPartyService(repository.NewMemory(), application.PartyOptions{})
	srv := handler.New(parties, application.NewChatService(parties, application.ChatOptions{}), uuidv7.NewString())
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
