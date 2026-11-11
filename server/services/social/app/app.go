// Package app is the social service's composition root, shared with the end-to-end tests.
package app

import (
	"context"
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"

	socialv1 "finga/api/gen/social/v1"
	"finga/internal/gamedata"
	"finga/internal/host"
	"finga/internal/telemetry"
	"finga/pkg/retention"
	"finga/pkg/uuidv7"
	"finga/services/social/config"
	"finga/services/social/internal/application"
	"finga/services/social/internal/handler"
	"finga/services/social/internal/repository"
)

// Build wires the social service; parties live in memory, so a restart forgets them.
func Build(_ context.Context, cfg config.Config, log *slog.Logger, reg *prometheus.Registry) (*host.Host, error) {
	data, err := gamedata.Load(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	h, err := host.New(host.Options{Name: "social", Settings: cfg.Host(), Logger: log, Registry: reg, Errors: handler.Errors})
	if err != nil {
		return nil, err
	}
	retentionMetrics, err := telemetry.NewRetentionMetrics(reg)
	if err != nil {
		h.Close()
		return nil, err
	}
	streams := handler.NewStreams(uuidv7.NewString())
	parties := application.NewPartyService(application.PartyDeps{Store: repository.NewMemory(), Notifier: streams})
	gone := retention.Policy{Keep: cfg.Members.Keep, Every: cfg.Members.PruneEvery, Batch: cfg.Members.PruneBatch, Gap: cfg.Members.PruneGap}
	h.Task("member retention", func(ctx context.Context) error {
		return retention.Run(ctx, "members", gone, parties.PruneMembers, retentionMetrics, log)
	})
	socialv1.RegisterSocialServiceServer(h.GRPC, handler.New(parties, application.NewChatService(application.ChatDeps{Parties: parties, LineMax: data.ChatLineMax()}), streams))
	return h, nil
}
