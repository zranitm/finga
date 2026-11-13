// Package app is the social service's composition root, shared with the end-to-end tests.
package app

import (
	"context"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/host"
	"finga/pkg/retention"
	"finga/pkg/retention/retentionprom"
	"finga/pkg/uuidv7"
	"finga/services/social/config"
	"finga/services/social/internal/application"
	"finga/services/social/internal/handler"
	"finga/services/social/internal/repository"
)

// Build adds the social module to h from cfg; parties live in memory, so a restart forgets them.
func Build(_ context.Context, h *host.Host, cfg config.Config) error {
	data, err := h.GameData()
	if err != nil {
		return err
	}
	m, err := h.Module("social")
	if err != nil {
		return err
	}
	retentionMetrics, err := retentionprom.New(h.Registry(), "social")
	if err != nil {
		return err
	}
	streams := handler.NewStreams(uuidv7.NewString())
	parties := application.NewPartyService(application.PartyDeps{Store: repository.NewMemory(), Notifier: streams})
	gone := retention.Policy{Keep: cfg.Members.Keep, Every: cfg.Members.PruneEvery, Batch: cfg.Members.PruneBatch, Gap: cfg.Members.PruneGap}
	m.Task("member retention", func(ctx context.Context) error {
		return retention.Run(ctx, "members", gone, parties.PruneMembers, retentionMetrics, m.Logger)
	}, host.Restartable)
	srv, err := m.ServeGRPC(handler.Errors)
	if err != nil {
		return err
	}
	socialv1.RegisterSocialServiceServer(srv, handler.New(parties, application.NewChatService(application.ChatDeps{Parties: parties, LineMax: data.ChatLineMax()}), streams, m.Panics))
	return nil
}
